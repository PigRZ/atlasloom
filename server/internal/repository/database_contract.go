// 数据库契约集中声明当前模型允许的 MongoDB 文档结构、索引和可追踪指纹。
package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	databaseContractID      = "current-model"
	databaseContractVersion = 1
)

type collectionContract struct {
	Name      string `json:"name"`
	Validator bson.D `json:"validator"`
}

type indexContract struct {
	Collection         string `json:"collection"`
	Name               string `json:"name"`
	Keys               bson.D `json:"keys"`
	Unique             bool   `json:"unique,omitempty"`
	Sparse             bool   `json:"sparse,omitempty"`
	ExpireAfterSeconds *int32 `json:"expireAfterSeconds,omitempty"`
	PartialFilter      bson.D `json:"partialFilter,omitempty"`
	CollationLocale    string `json:"collationLocale,omitempty"`
	CollationStrength  int    `json:"collationStrength,omitempty"`
}

type storedDatabaseContract struct {
	ID              string    `bson:"_id"`
	ContractVersion int       `bson:"contract_version"`
	Fingerprint     string    `bson:"fingerprint"`
	UpdatedAt       time.Time `bson:"updated_at"`
}

func (contract indexContract) model() mongo.IndexModel {
	builder := options.Index().SetName(contract.Name)
	if contract.Unique {
		builder.SetUnique(true)
	}
	if contract.Sparse {
		builder.SetSparse(true)
	}
	if contract.ExpireAfterSeconds != nil {
		builder.SetExpireAfterSeconds(*contract.ExpireAfterSeconds)
	}
	if len(contract.PartialFilter) > 0 {
		builder.SetPartialFilterExpression(contract.PartialFilter)
	}
	if contract.CollationLocale != "" {
		builder.SetCollation(&options.Collation{
			Locale:   contract.CollationLocale,
			Strength: contract.CollationStrength,
		})
	}
	return mongo.IndexModel{Keys: contract.Keys, Options: builder}
}

// CurrentDatabaseContractFingerprint changes whenever a collection validator or
// managed index changes. It is persisted only after every prerequisite succeeds.
func CurrentDatabaseContractFingerprint() string {
	payload, err := json.Marshal(struct {
		Version     int                  `json:"version"`
		Collections []collectionContract `json:"collections"`
		Indexes     []indexContract      `json:"indexes"`
	}{
		Version:     databaseContractVersion,
		Collections: databaseCollectionContracts(),
		Indexes:     databaseIndexContracts(),
	})
	if err != nil {
		panic(fmt.Sprintf("database contract cannot be encoded: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (r *Repository) initializeDatabaseContract(ctx context.Context) error {
	if err := r.ensureCollectionContracts(ctx); err != nil {
		return err
	}
	if err := r.ensureIndexContracts(ctx); err != nil {
		return err
	}
	if err := r.ensureLibraryState(ctx); err != nil {
		return err
	}
	return r.storeDatabaseContract(ctx)
}

func (r *Repository) ensureCollectionContracts(ctx context.Context) error {
	names, err := r.db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return fmt.Errorf("list MongoDB collections: %w", err)
	}
	existing := make(map[string]struct{}, len(names))
	for _, name := range names {
		existing[name] = struct{}{}
	}

	// 先只读验证全部已有集合，避免后续集合校验失败时留下半套 validator。
	for _, contract := range databaseCollectionContracts() {
		if _, ok := existing[contract.Name]; !ok {
			continue
		}
		err := r.collection(contract.Name).FindOne(
			ctx,
			bson.D{{Key: "$nor", Value: bson.A{contract.Validator}}},
			options.FindOne().SetProjection(bson.D{{Key: "_id", Value: 1}}),
		).Err()
		if err == nil {
			return fmt.Errorf(
				"collection %s contains a document outside the current model; "+
					"validate a complete local snapshot before changing the contract",
				contract.Name,
			)
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("validate collection %s: %w", contract.Name, err)
		}
	}

	for _, contract := range databaseCollectionContracts() {
		if _, ok := existing[contract.Name]; !ok {
			err := r.db.CreateCollection(
				ctx,
				contract.Name,
				options.CreateCollection().
					SetValidator(contract.Validator).
					SetValidationLevel("strict").
					SetValidationAction("error"),
			)
			if err != nil {
				return fmt.Errorf("create collection %s: %w", contract.Name, err)
			}
			continue
		}
		command := bson.D{
			{Key: "collMod", Value: contract.Name},
			{Key: "validator", Value: contract.Validator},
			{Key: "validationLevel", Value: "strict"},
			{Key: "validationAction", Value: "error"},
		}
		if err := r.db.RunCommand(ctx, command).Err(); err != nil {
			return fmt.Errorf("apply validator to %s: %w", contract.Name, err)
		}
	}
	return nil
}

func (r *Repository) ensureIndexContracts(ctx context.Context) error {
	grouped := make(map[string][]mongo.IndexModel)
	expected := make(map[string]map[string]struct{})
	for _, collection := range databaseCollectionContracts() {
		expected[collection.Name] = map[string]struct{}{"_id_": {}}
	}
	for _, contract := range databaseIndexContracts() {
		grouped[contract.Collection] = append(
			grouped[contract.Collection],
			contract.model(),
		)
		expected[contract.Collection][contract.Name] = struct{}{}
	}
	for collection, names := range expected {
		if models := grouped[collection]; len(models) > 0 {
			if _, err := r.collection(collection).Indexes().CreateMany(ctx, models); err != nil {
				return fmt.Errorf("ensure indexes for %s: %w", collection, err)
			}
		}
		cursor, err := r.collection(collection).Indexes().List(ctx)
		if err != nil {
			return fmt.Errorf("list indexes for %s: %w", collection, err)
		}
		var installed []struct {
			Name string `bson:"name"`
		}
		if err := cursor.All(ctx, &installed); err != nil {
			return fmt.Errorf("read indexes for %s: %w", collection, err)
		}
		for _, index := range installed {
			if _, ok := names[index.Name]; !ok {
				return fmt.Errorf(
					"collection %s has unmanaged index %s; "+
						"verify current snapshots locally before removing it",
					collection,
					index.Name,
				)
			}
		}
		if err := cursor.Close(ctx); err != nil {
			return fmt.Errorf("close index cursor for %s: %w", collection, err)
		}
	}
	return nil
}

func (r *Repository) storeDatabaseContract(ctx context.Context) error {
	_, err := r.collection("system_metadata").UpdateOne(
		ctx,
		bson.D{{Key: "_id", Value: databaseContractID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "contract_version", Value: databaseContractVersion},
			{Key: "fingerprint", Value: CurrentDatabaseContractFingerprint()},
			{Key: "updated_at", Value: time.Now().UTC()},
		}}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("store database contract fingerprint: %w", err)
	}
	return nil
}

// DatabaseContractFingerprint reads the installed fingerprint for health and
// integration checks without exposing any business data.
func (r *Repository) DatabaseContractFingerprint(ctx context.Context) (string, error) {
	var contract storedDatabaseContract
	err := r.collection("system_metadata").FindOne(
		ctx,
		bson.D{{Key: "_id", Value: databaseContractID}},
	).Decode(&contract)
	if err != nil {
		return "", err
	}
	if contract.ContractVersion != databaseContractVersion {
		return "", fmt.Errorf(
			"database contract version is %d, expected %d",
			contract.ContractVersion,
			databaseContractVersion,
		)
	}
	return contract.Fingerprint, nil
}

func databaseCollectionContracts() []collectionContract {
	return []collectionContract{
		{Name: "trips", Validator: jsonSchema(tripDocumentSchema())},
		{Name: "records", Validator: jsonSchema(recordDocumentSchema())},
		{Name: "library_state", Validator: jsonSchema(libraryStateDocumentSchema())},
		{Name: "library_records", Validator: jsonSchema(libraryRecordDocumentSchema())},
		{Name: "member_credentials", Validator: jsonSchema(memberCredentialDocumentSchema())},
		{Name: "memberships", Validator: jsonSchema(membershipDocumentSchema())},
		{Name: "sessions", Validator: jsonSchema(sessionDocumentSchema())},
		{Name: "audit_logs", Validator: jsonSchema(auditLogDocumentSchema())},
		{Name: "snapshots", Validator: jsonSchema(snapshotDocumentSchema(true))},
		{Name: "library_snapshots", Validator: jsonSchema(snapshotDocumentSchema(false))},
		{Name: "idempotency_requests", Validator: jsonSchema(idempotencyDocumentSchema())},
		{Name: "system_metadata", Validator: jsonSchema(systemMetadataDocumentSchema())},
	}
}

func databaseIndexContracts() []indexContract {
	zero := int32(0)
	return []indexContract{
		{
			Collection: "trips", Name: "active_start_date",
			Keys: bson.D{
				{Key: "deleted_at", Value: 1},
				{Key: "start_date", Value: 1},
			},
		},
		{
			Collection: "records", Name: "trip_resource_record_key",
			Keys: bson.D{
				{Key: "trip_id", Value: 1},
				{Key: "resource", Value: 1},
				{Key: "record_key", Value: 1},
			},
			Unique: true,
		},
		{
			Collection: "records", Name: "trip_resource_group_position",
			Keys: bson.D{
				{Key: "trip_id", Value: 1},
				{Key: "resource", Value: 1},
				{Key: "group_key", Value: 1},
				{Key: "position", Value: 1},
			},
			Unique: true,
		},
		{
			Collection: "records", Name: "global_member_reference",
			Keys: bson.D{
				{Key: "resource", Value: 1},
				{Key: "data.memberId", Value: 1},
				{Key: "trip_id", Value: 1},
			},
			PartialFilter: bson.D{
				{Key: "resource", Value: "members"},
				{Key: "data.memberId", Value: bson.D{{Key: "$type", Value: "string"}}},
			},
		},
		{
			Collection: "memberships", Name: "trip_member",
			Keys: bson.D{
				{Key: "trip_id", Value: 1},
				{Key: "member_id", Value: 1},
			},
			Unique: true,
		},
		{
			Collection: "library_records", Name: "library_resource_record_key",
			Keys: bson.D{
				{Key: "resource", Value: 1},
				{Key: "record_key", Value: 1},
			},
			Unique: true,
		},
		{
			Collection: "library_records", Name: "library_resource_position",
			Keys: bson.D{
				{Key: "resource", Value: 1},
				{Key: "position", Value: 1},
			},
			Unique: true,
		},
		{
			Collection: "library_records", Name: "library_member_mobile",
			Keys:   bson.D{{Key: "data.mobile", Value: 1}},
			Unique: true,
			Sparse: true,
		},
		{
			Collection: "library_records", Name: "library_tag_category_name",
			Keys: bson.D{
				{Key: "resource", Value: 1},
				{Key: "data.category", Value: 1},
				{Key: "data.name", Value: 1},
			},
			Unique:            true,
			PartialFilter:     bson.D{{Key: "resource", Value: "tags"}},
			CollationLocale:   "zh",
			CollationStrength: 2,
		},
		{
			Collection:         "sessions",
			Name:               "expires_at",
			Keys:               bson.D{{Key: "expires_at", Value: 1}},
			ExpireAfterSeconds: &zero,
		},
		{
			Collection: "audit_logs", Name: "trip_created_at",
			Keys: bson.D{
				{Key: "trip_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
		},
		{
			Collection: "snapshots", Name: "trip_created_at",
			Keys: bson.D{
				{Key: "trip_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
		},
		{
			Collection: "snapshots", Name: "mirror_state_created_at",
			Keys: bson.D{
				{Key: "mirror_state", Value: 1},
				{Key: "created_at", Value: 1},
			},
		},
		{
			Collection: "library_snapshots", Name: "created_at",
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
		{
			Collection: "library_snapshots", Name: "mirror_state_created_at",
			Keys: bson.D{
				{Key: "mirror_state", Value: 1},
				{Key: "created_at", Value: 1},
			},
		},
		{
			Collection: "idempotency_requests", Name: "scope_key",
			Keys: bson.D{
				{Key: "scope", Value: 1},
				{Key: "key", Value: 1},
			},
			Unique: true,
		},
		{
			Collection:         "idempotency_requests",
			Name:               "expires_at",
			Keys:               bson.D{{Key: "expires_at", Value: 1}},
			ExpireAfterSeconds: &zero,
		},
	}
}

func jsonSchema(schema bson.D) bson.D {
	return bson.D{{Key: "$jsonSchema", Value: schema}}
}

func objectSchema(required []string, properties bson.D) bson.D {
	return bson.D{
		{Key: "bsonType", Value: "object"},
		{Key: "required", Value: required},
		{Key: "additionalProperties", Value: false},
		{Key: "properties", Value: properties},
	}
}

func stringProperty() bson.D {
	return bson.D{{Key: "bsonType", Value: "string"}}
}

func recordIDProperty() bson.D {
	return bson.D{
		{Key: "bsonType", Value: "string"},
		{Key: "maxLength", Value: 128},
		{Key: "pattern", Value: `^[A-Za-z0-9](?:[A-Za-z0-9_:-]|\.[A-Za-z0-9_:-])*\.?$`},
	}
}

func stringEnum(values ...string) bson.D {
	items := make(bson.A, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return bson.D{
		{Key: "bsonType", Value: "string"},
		{Key: "enum", Value: items},
	}
}

func longProperty(minimum int64) bson.D {
	return bson.D{
		{Key: "bsonType", Value: "long"},
		{Key: "minimum", Value: minimum},
	}
}

func numberProperty(minimum *float64, maximum *float64) bson.D {
	schema := bson.D{
		{Key: "bsonType", Value: bson.A{"int", "long", "double", "decimal"}},
	}
	if minimum != nil {
		schema = append(schema, bson.E{Key: "minimum", Value: *minimum})
	}
	if maximum != nil {
		schema = append(schema, bson.E{Key: "maximum", Value: *maximum})
	}
	return schema
}

func stringArrayProperty() bson.D {
	return bson.D{
		{Key: "bsonType", Value: "array"},
		{Key: "items", Value: stringProperty()},
	}
}

func tripDocumentSchema() bson.D {
	return objectSchema(
		[]string{
			"_id", "title", "summary", "start_date", "end_date",
			"timezone", "currency", "version",
			"created_at", "updated_at",
		},
		bson.D{
			{Key: "_id", Value: stringProperty()},
			{Key: "title", Value: stringProperty()},
			{Key: "summary", Value: stringProperty()},
			{Key: "start_date", Value: bson.D{
				{Key: "bsonType", Value: "string"},
				{Key: "pattern", Value: `^\d{4}-\d{2}-\d{2}$`},
			}},
			{Key: "end_date", Value: bson.D{
				{Key: "bsonType", Value: "string"},
				{Key: "pattern", Value: `^\d{4}-\d{2}-\d{2}$`},
			}},
			{Key: "timezone", Value: stringProperty()},
			{Key: "currency", Value: stringProperty()},
			{Key: "version", Value: longProperty(1)},
			{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
			{Key: "updated_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
			{Key: "deleted_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
			{Key: "deleted_by_member_id", Value: stringProperty()},
		},
	)
}

func recordDocumentSchema() bson.D {
	schema := objectSchema(
		[]string{
			"_id", "trip_id", "resource", "record_key", "group_key", "position",
			"version", "data", "created_by_member_id", "updated_by_member_id",
			"created_at", "updated_at",
		},
		bson.D{
			{Key: "_id", Value: recordIDProperty()},
			{Key: "trip_id", Value: stringProperty()},
			{Key: "resource", Value: stringEnum(
				"days", "places", "segments", "bookings", "expenses",
				"packing-items", "research-evidence", "members",
			)},
			{Key: "record_key", Value: recordIDProperty()},
			{Key: "group_key", Value: stringProperty()},
			{Key: "position", Value: longProperty(0)},
			{Key: "version", Value: longProperty(1)},
			{Key: "data", Value: bson.D{{Key: "bsonType", Value: "object"}}},
			{Key: "created_by_member_id", Value: recordIDProperty()},
			{Key: "updated_by_member_id", Value: recordIDProperty()},
			{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
			{Key: "updated_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
	schema = append(schema, bson.E{
		Key: "oneOf",
		Value: bson.A{
			recordResourceBranch("days", dayDataSchema()),
			recordResourceBranch("places", placeDataSchema()),
			recordResourceBranch("segments", segmentDataSchema()),
			recordResourceBranch("bookings", bookingDataSchema()),
			recordResourceBranch("expenses", expenseDataSchema()),
			recordResourceBranch("packing-items", packingDataSchema()),
			recordResourceBranch("research-evidence", researchDataSchema()),
			recordResourceBranch("members", tripMemberDataSchema()),
		},
	})
	return schema
}

func recordResourceBranch(resource string, data bson.D) bson.D {
	return bson.D{
		{Key: "required", Value: bson.A{"resource", "data"}},
		{Key: "properties", Value: bson.D{
			{Key: "resource", Value: stringEnum(resource)},
			{Key: "data", Value: data},
		}},
	}
}

func dataSchema(required []string, properties bson.D) bson.D {
	return objectSchema(required, properties)
}

func dayDataSchema() bson.D {
	return dataSchema(
		[]string{"title", "date"},
		bson.D{
			{Key: "title", Value: stringProperty()},
			{Key: "date", Value: bson.D{
				{Key: "bsonType", Value: "string"},
				{Key: "pattern", Value: `^\d{4}-\d{2}-\d{2}$`},
			}},
			{Key: "placeEntries", Value: bson.D{
				{Key: "bsonType", Value: "array"},
				{Key: "items", Value: objectSchema(
					[]string{"placeId"},
					bson.D{
						{Key: "placeId", Value: stringProperty()},
						{Key: "arrivalTime", Value: stringProperty()},
						{Key: "departureTime", Value: stringProperty()},
						{Key: "additionalInfo", Value: stringProperty()},
					},
				)},
			}},
			{Key: "additionalInfo", Value: stringProperty()},
		},
	)
}

func placeDataSchema() bson.D {
	latitudeMin, latitudeMax := -90.0, 90.0
	longitudeMin, longitudeMax := -180.0, 180.0
	return dataSchema(
		[]string{"name"},
		bson.D{
			{Key: "name", Value: stringProperty()},
			{Key: "type", Value: stringProperty()},
			{Key: "address", Value: stringProperty()},
			{Key: "region", Value: stringProperty()},
			{Key: "latitude", Value: numberProperty(&latitudeMin, &latitudeMax)},
			{Key: "longitude", Value: numberProperty(&longitudeMin, &longitudeMax)},
			{Key: "weatherAdcode", Value: bson.D{
				{Key: "bsonType", Value: "string"},
				{Key: "pattern", Value: `^$|^\d{6}$`},
			}},
			{Key: "navigationURL", Value: stringProperty()},
			{Key: "openingHours", Value: stringProperty()},
			{Key: "recommendedTicketPlatform", Value: stringProperty()},
		},
	)
}

func segmentDataSchema() bson.D {
	zero := 0.0
	return dataSchema(
		[]string{"dayId", "fromPlaceId", "toPlaceId", "mode"},
		bson.D{
			{Key: "mode", Value: stringEnum(
				"walk", "drive", "taxi", "rideshare", "public_transit", "train",
				"flight", "shuttle", "scenic_bus", "ferry", "bicycle", "other",
			)},
			{Key: "dayId", Value: stringProperty()},
			{Key: "fromPlaceId", Value: stringProperty()},
			{Key: "toPlaceId", Value: stringProperty()},
			{Key: "departureTime", Value: stringProperty()},
			{Key: "durationMinutes", Value: numberProperty(&zero, nil)},
			{Key: "distanceMeters", Value: numberProperty(&zero, nil)},
			{Key: "navigationURL", Value: stringProperty()},
			{Key: "additionalInfo", Value: stringProperty()},
		},
	)
}

func bookingDataSchema() bson.D {
	return dataSchema(
		[]string{"item"},
		bson.D{
			{Key: "item", Value: stringProperty()},
			{Key: "type", Value: stringProperty()},
			{Key: "bookerMemberId", Value: stringProperty()},
			{Key: "placeId", Value: stringProperty()},
			{Key: "platformId", Value: stringProperty()},
			{Key: "dateTime", Value: stringProperty()},
			{Key: "status", Value: stringProperty()},
			{Key: "link", Value: stringProperty()},
			{Key: "notes", Value: stringProperty()},
		},
	)
}

func expenseDataSchema() bson.D {
	zero := 0.0
	return dataSchema(
		[]string{"item", "type", "splitMode", "splitMemberIds"},
		bson.D{
			{Key: "item", Value: stringProperty()},
			{Key: "occurredAt", Value: stringProperty()},
			{Key: "type", Value: stringProperty()},
			{Key: "amountMinor", Value: numberProperty(&zero, nil)},
			{Key: "payerMemberId", Value: stringProperty()},
			{Key: "splitMode", Value: stringEnum("all", "selected")},
			{Key: "splitMemberIds", Value: stringArrayProperty()},
			{Key: "bookingRecordId", Value: stringProperty()},
			{Key: "notes", Value: stringProperty()},
		},
	)
}

func packingDataSchema() bson.D {
	zero := 0.0
	return dataSchema(
		[]string{"item", "ownerMemberId"},
		bson.D{
			{Key: "item", Value: stringProperty()},
			{Key: "category", Value: stringProperty()},
			{Key: "dayIds", Value: stringArrayProperty()},
			{Key: "ownerMemberId", Value: stringProperty()},
			{Key: "quantity", Value: numberProperty(&zero, nil)},
			{Key: "packed", Value: bson.D{{Key: "bsonType", Value: "bool"}}},
			{Key: "notes", Value: stringProperty()},
		},
	)
}

func researchDataSchema() bson.D {
	return dataSchema(
		[]string{"title"},
		bson.D{
			{Key: "title", Value: stringProperty()},
			{Key: "content", Value: stringProperty()},
			{Key: "platformId", Value: stringProperty()},
			{Key: "recordedByMemberId", Value: stringProperty()},
			{Key: "dayIds", Value: stringArrayProperty()},
			{Key: "placeId", Value: stringProperty()},
		},
	)
}

func tripMemberDataSchema() bson.D {
	return dataSchema(
		[]string{"memberId"},
		bson.D{
			{Key: "memberId", Value: stringProperty()},
			{Key: "role", Value: stringProperty()},
			{Key: "note", Value: stringProperty()},
		},
	)
}

func libraryStateDocumentSchema() bson.D {
	return objectSchema(
		[]string{"_id", "version", "updated_at"},
		bson.D{
			{Key: "_id", Value: stringEnum(libraryStateID)},
			{Key: "version", Value: longProperty(1)},
			{Key: "reference_epoch", Value: longProperty(0)},
			{Key: "updated_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
}

func libraryRecordDocumentSchema() bson.D {
	schema := objectSchema(
		[]string{
			"_id", "resource", "record_key", "position", "version", "data",
			"created_at", "updated_at",
		},
		bson.D{
			{Key: "_id", Value: recordIDProperty()},
			{Key: "resource", Value: stringEnum("members", "tags")},
			{Key: "record_key", Value: recordIDProperty()},
			{Key: "position", Value: longProperty(0)},
			{Key: "version", Value: longProperty(1)},
			{Key: "data", Value: bson.D{{Key: "bsonType", Value: "object"}}},
			{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
			{Key: "updated_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
	schema = append(schema, bson.E{
		Key: "oneOf",
		Value: bson.A{
			recordResourceBranch("members", libraryMemberDataSchema()),
			recordResourceBranch("tags", libraryTagDataSchema()),
		},
	})
	return schema
}

func libraryMemberDataSchema() bson.D {
	return dataSchema(
		[]string{"displayName"},
		bson.D{
			{Key: "displayName", Value: stringProperty()},
			{Key: "mobile", Value: stringProperty()},
			{Key: "alternateContact", Value: stringProperty()},
			{Key: "emergencyContact", Value: stringProperty()},
			{Key: "admin", Value: bson.D{{Key: "bsonType", Value: "bool"}}},
			{Key: "status", Value: stringProperty()},
			{Key: "note", Value: stringProperty()},
		},
	)
}

func libraryTagDataSchema() bson.D {
	return dataSchema(
		[]string{"name", "category"},
		bson.D{
			{Key: "name", Value: stringProperty()},
			{Key: "category", Value: stringEnum(
				"general", "place-type", "expense-type", "platform-type",
			)},
			{Key: "color", Value: stringProperty()},
			{Key: "status", Value: stringProperty()},
		},
	)
}

func memberCredentialDocumentSchema() bson.D {
	return objectSchema(
		[]string{"_id", "password_hash"},
		bson.D{
			{Key: "_id", Value: stringProperty()},
			{Key: "password_hash", Value: stringProperty()},
		},
	)
}

func membershipDocumentSchema() bson.D {
	return objectSchema(
		[]string{"_id", "trip_id", "member_id", "role", "created_at"},
		bson.D{
			{Key: "_id", Value: stringProperty()},
			{Key: "trip_id", Value: stringProperty()},
			{Key: "member_id", Value: stringProperty()},
			{Key: "role", Value: stringEnum("owner", "editor", "viewer")},
			{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
}

func sessionDocumentSchema() bson.D {
	return objectSchema(
		[]string{"_id", "member_id", "csrf_token", "expires_at"},
		bson.D{
			{Key: "_id", Value: stringProperty()},
			{Key: "member_id", Value: stringProperty()},
			{Key: "csrf_token", Value: stringProperty()},
			{Key: "expires_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
}

func auditLogDocumentSchema() bson.D {
	return objectSchema(
		[]string{"_id", "member_id", "module", "action", "created_at"},
		bson.D{
			{Key: "_id", Value: stringProperty()},
			{Key: "trip_id", Value: stringProperty()},
			{Key: "member_id", Value: stringProperty()},
			{Key: "module", Value: stringProperty()},
			{Key: "action", Value: stringProperty()},
			{Key: "resource", Value: stringProperty()},
			{Key: "record_id", Value: stringProperty()},
			{Key: "record_label", Value: stringProperty()},
			{Key: "before", Value: bson.D{{Key: "bsonType", Value: "object"}}},
			{Key: "after", Value: bson.D{{Key: "bsonType", Value: "object"}}},
			{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
}

func snapshotDocumentSchema(tripBound bool) bson.D {
	required := []string{
		"_id", "revision", "reason", "payload", "sha256", "content_checksum",
		"mirror_state", "created_at",
	}
	properties := bson.D{{Key: "_id", Value: stringProperty()}}
	if tripBound {
		required = append(required, "trip_id")
		properties = append(properties, bson.E{Key: "trip_id", Value: stringProperty()})
	}
	properties = append(properties,
		bson.E{Key: "revision", Value: longProperty(1)},
		bson.E{Key: "reason", Value: stringProperty()},
		bson.E{Key: "payload", Value: bson.D{{Key: "bsonType", Value: "binData"}}},
		bson.E{Key: "sha256", Value: bson.D{
			{Key: "bsonType", Value: "string"},
			{Key: "pattern", Value: `^[a-f0-9]{64}$`},
		}},
		bson.E{Key: "content_checksum", Value: bson.D{
			{Key: "bsonType", Value: "string"},
			{Key: "pattern", Value: `^[a-f0-9]{64}$`},
		}},
		bson.E{Key: "mirror_state", Value: stringEnum("pending", "mirrored")},
		bson.E{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
	)
	return objectSchema(required, properties)
}

func idempotencyDocumentSchema() bson.D {
	return objectSchema(
		[]string{
			"_id", "scope", "key", "request_hash", "response", "created_at",
			"expires_at",
		},
		bson.D{
			{Key: "_id", Value: stringProperty()},
			{Key: "scope", Value: stringProperty()},
			{Key: "key", Value: stringProperty()},
			{Key: "request_hash", Value: stringProperty()},
			{Key: "response", Value: bson.D{{Key: "bsonType", Value: "binData"}}},
			{Key: "created_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
			{Key: "expires_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
}

func systemMetadataDocumentSchema() bson.D {
	return objectSchema(
		[]string{"_id", "contract_version", "fingerprint", "updated_at"},
		bson.D{
			{Key: "_id", Value: stringEnum(databaseContractID)},
			{Key: "contract_version", Value: bson.D{
				{Key: "bsonType", Value: bson.A{"int", "long"}},
				{Key: "minimum", Value: 1},
			}},
			{Key: "fingerprint", Value: bson.D{
				{Key: "bsonType", Value: "string"},
				{Key: "pattern", Value: `^[a-f0-9]{64}$`},
			}},
			{Key: "updated_at", Value: bson.D{{Key: "bsonType", Value: "date"}}},
		},
	)
}
