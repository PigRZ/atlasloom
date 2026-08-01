// Repository 是唯一 MongoDB 访问层，并提供 Trip 隔离、事务和完整快照构建。
package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"my-travel/server/internal/app"
	"my-travel/server/internal/model"
)

var (
	ErrNotFound              = errors.New("not found")
	ErrForbidden             = errors.New("forbidden")
	ErrConflict              = errors.New("version conflict")
	ErrReferenced            = errors.New("resource is still referenced")
	ErrIdempotencyConflict   = errors.New("idempotency key conflict")
	ErrSnapshotIdentity      = errors.New("snapshot changes immutable record identity")
	ErrSnapshotValidation    = errors.New("snapshot validation failed")
	ErrRepositoryUnavailable = errors.New("repository unavailable")
)

type Repository struct {
	client *mongo.Client
	db     *mongo.Database
}

type ResourceDiff struct {
	Current  int `json:"current"`
	Incoming int `json:"incoming"`
	Created  int `json:"created"`
	Updated  int `json:"updated"`
	Deleted  int `json:"deleted"`
}

type TripFieldDiff struct {
	Current  string `json:"current"`
	Incoming string `json:"incoming"`
}

type MembershipAccessChange struct {
	MemberID string `json:"memberId"`
	Role     string `json:"role"`
}

type MembershipAccessDiff struct {
	Created []MembershipAccessChange `json:"created"`
	Deleted []MembershipAccessChange `json:"deleted"`
}

type SnapshotDiff struct {
	BaseRevision     int64                    `json:"baseRevision"`
	CurrentRevision  int64                    `json:"currentRevision"`
	Checksum         string                   `json:"checksum"`
	TripChanges      map[string]TripFieldDiff `json:"tripChanges"`
	Resources        map[string]ResourceDiff  `json:"resources"`
	MembershipAccess MembershipAccessDiff     `json:"membershipAccess"`
	DeleteCount      int                      `json:"deleteCount"`
	HasChanges       bool                     `json:"hasChanges"`
	Message          string                   `json:"message"`
}

type RestorePreview struct {
	SnapshotID string `json:"snapshotId"`
	SHA256     string `json:"sha256"`
	SnapshotDiff
}

type recordIdentity struct {
	Resource  model.Resource
	RecordKey string
}

const (
	SnapshotNoChangesMessage = "已同步，无需写入"
	SnapshotChangesMessage   = "检测到待应用变更"
)

func (diff *SnapshotDiff) Finalize() {
	diff.HasChanges = len(diff.TripChanges) > 0
	for _, resource := range diff.Resources {
		if resource.Created > 0 || resource.Updated > 0 || resource.Deleted > 0 {
			diff.HasChanges = true
			break
		}
	}
	if len(diff.MembershipAccess.Created) > 0 ||
		len(diff.MembershipAccess.Deleted) > 0 {
		diff.HasChanges = true
	}
	if diff.HasChanges {
		diff.Message = SnapshotChangesMessage
	} else {
		diff.Message = SnapshotNoChangesMessage
	}
}

func DiffTripMetadata(current, incoming model.Trip) map[string]TripFieldDiff {
	changes := map[string]TripFieldDiff{}
	compare := func(field, currentValue, incomingValue string) {
		if currentValue != incomingValue {
			changes[field] = TripFieldDiff{Current: currentValue, Incoming: incomingValue}
		}
	}
	compare("title", current.Title, incoming.Title)
	compare("summary", current.Summary, incoming.Summary)
	compare("startDate", current.StartDate, incoming.StartDate)
	compare("endDate", current.EndDate, incoming.EndDate)
	compare("timezone", current.Timezone, incoming.Timezone)
	compare("currency", current.Currency, incoming.Currency)
	return changes
}

// ValidateRecordIdentityTransition prevents a delete-and-create payload from
// reusing an existing immutable ID for another resource or recordKey.
func ValidateRecordIdentityTransition(
	current, incoming map[string][]model.Record,
) error {
	currentByID := make(map[string]recordIdentity)
	for _, resource := range model.Resources() {
		for _, row := range current[string(resource)] {
			identity := recordIdentity{Resource: resource, RecordKey: row.RecordKey}
			if _, exists := currentByID[row.ID]; exists {
				return fmt.Errorf("%w: current record id %s is not globally unique", ErrSnapshotIdentity, row.ID)
			}
			currentByID[row.ID] = identity
		}
	}
	incomingByID := make(map[string]recordIdentity)
	for _, resource := range model.Resources() {
		for _, row := range incoming[string(resource)] {
			identity := recordIdentity{Resource: resource, RecordKey: row.RecordKey}
			if _, exists := incomingByID[row.ID]; exists {
				return fmt.Errorf("%w: incoming record id %s is not globally unique", ErrSnapshotIdentity, row.ID)
			}
			incomingByID[row.ID] = identity
			if previous, exists := currentByID[row.ID]; exists && previous != identity {
				return fmt.Errorf(
					"%w: record id %s changed identity from %s/%s to %s/%s",
					ErrSnapshotIdentity,
					row.ID,
					previous.Resource,
					previous.RecordKey,
					identity.Resource,
					identity.RecordKey,
				)
			}
		}
	}
	return nil
}

type recordReference struct {
	Field  string
	Target model.Resource
	IDs    []string
}

func Connect(ctx context.Context, config app.Config) (*Repository, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(config.MongoURI))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return &Repository{client: client, db: client.Database(config.MongoDatabase)}, nil
}

func (r *Repository) Close(ctx context.Context) error { return r.client.Disconnect(ctx) }

func (r *Repository) collection(name string) *mongo.Collection { return r.db.Collection(name) }

func (r *Repository) Initialize(ctx context.Context) error {
	return r.initializeDatabaseContract(ctx)
}

func (r *Repository) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	return r.withTransaction(ctx, fn)
}

func (r *Repository) withSnapshotReadTransaction(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	return r.withTransaction(
		ctx,
		fn,
		options.Transaction().SetReadConcern(readconcern.Snapshot()),
	)
}

func (r *Repository) withTransaction(
	ctx context.Context,
	fn func(context.Context) error,
	transactionOptions ...options.Lister[options.TransactionOptions],
) error {
	session, err := r.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		return nil, fn(tx)
	}, transactionOptions...)
	return err
}

func (r *Repository) ListTrips(ctx context.Context) ([]model.Trip, error) {
	cursor, err := r.collection("trips").Find(
		ctx,
		activeTripFilter(""),
		options.Find().SetSort(bson.D{{Key: "start_date", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	trips := make([]model.Trip, 0)
	if err := cursor.All(ctx, &trips); err != nil {
		return nil, err
	}
	return trips, nil
}

func (r *Repository) GetTrip(ctx context.Context, tripID string) (model.Trip, error) {
	return r.getTrip(ctx, activeTripFilter(tripID))
}

func (r *Repository) getTripIncludingDeleted(ctx context.Context, tripID string) (model.Trip, error) {
	return r.getTrip(ctx, bson.M{"_id": tripID})
}

func (r *Repository) getTrip(ctx context.Context, filter bson.M) (model.Trip, error) {
	var trip model.Trip
	err := r.collection("trips").FindOne(ctx, filter).Decode(&trip)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Trip{}, ErrNotFound
	}
	if err != nil {
		return model.Trip{}, err
	}
	return trip, nil
}

func activeTripFilter(tripID string) bson.M {
	filter := bson.M{"deleted_at": bson.M{"$exists": false}}
	if tripID != "" {
		filter["_id"] = tripID
	}
	return filter
}

func (r *Repository) CreateTrip(ctx context.Context, trip model.Trip) (model.Trip, error) {
	if err := trip.Validate(); err != nil {
		return model.Trip{}, err
	}
	now := time.Now().UTC()
	trip.CreatedAt, trip.UpdatedAt, trip.Version = now, now, 1
	trip.DeletedAt, trip.DeletedByMemberID = nil, ""
	if trip.Timezone == "" {
		trip.Timezone = "Asia/Shanghai"
	}
	if trip.Currency == "" {
		trip.Currency = "CNY"
	}
	if _, err := r.collection("trips").InsertOne(ctx, trip); mongo.IsDuplicateKeyError(err) {
		return model.Trip{}, fmt.Errorf("trip id already exists")
	} else {
		return trip, err
	}
}

func (r *Repository) DeleteTrip(
	ctx context.Context,
	tripID string,
	version int64,
	actorMemberID string,
) (model.Trip, error) {
	current, err := r.GetTrip(ctx, tripID)
	if err != nil {
		return model.Trip{}, err
	}
	if version < 1 || current.Version != version {
		return model.Trip{}, ErrConflict
	}
	now := time.Now().UTC()
	var deleted model.Trip
	err = r.collection("trips").FindOneAndUpdate(
		ctx,
		bson.M{
			"_id": tripID, "version": version,
			"deleted_at": bson.M{"$exists": false},
		},
		bson.M{
			"$set": bson.M{
				"deleted_at": now, "deleted_by_member_id": actorMemberID,
				"updated_at": now,
			},
			"$inc": bson.M{"version": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&deleted)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Trip{}, ErrConflict
	}
	return deleted, err
}

func (r *Repository) UpdateTrip(ctx context.Context, tripID string, version int64, patch map[string]any) (model.Trip, error) {
	if version < 1 {
		return model.Trip{}, ErrConflict
	}
	current, err := r.GetTrip(ctx, tripID)
	if err != nil {
		return model.Trip{}, err
	}
	if current.Version != version {
		return model.Trip{}, ErrConflict
	}
	set, err := validatedTripPatch(current, patch)
	if err != nil {
		return model.Trip{}, err
	}
	days, err := r.listRecords(ctx, tripID, model.Days)
	if err != nil {
		return model.Trip{}, err
	}
	targetStart, targetEnd := current.StartDate, current.EndDate
	if value, ok := patch["startDate"].(string); ok {
		targetStart = value
	}
	if value, ok := patch["endDate"].(string); ok {
		targetEnd = value
	}
	if err := validateTripDayBounds(targetStart, targetEnd, days); err != nil {
		return model.Trip{}, err
	}
	set["updated_at"] = time.Now().UTC()
	var result model.Trip
	err = r.collection("trips").FindOneAndUpdate(
		ctx,
		bson.M{"_id": tripID, "version": version},
		bson.M{"$set": set, "$inc": bson.M{"version": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Trip{}, ErrConflict
	}
	if err != nil {
		return model.Trip{}, err
	}
	return result, nil
}

func validateTripDayBounds(startDate, endDate string, days []model.Record) error {
	for _, day := range days {
		date, _ := day.Data["date"].(string)
		if date != "" && (date < startDate || date > endDate) {
			return fmt.Errorf(
				"旅行日期范围不能排除已有执行日 %s；请先调整或删除该执行日",
				date,
			)
		}
	}
	return nil
}

func validatedTripPatch(current model.Trip, patch map[string]any) (bson.M, error) {
	allowed := map[string]struct {
		bsonName string
		assign   func(string)
	}{
		"title":     {"title", func(value string) { current.Title = value }},
		"summary":   {"summary", func(value string) { current.Summary = value }},
		"startDate": {"start_date", func(value string) { current.StartDate = value }},
		"endDate":   {"end_date", func(value string) { current.EndDate = value }},
		"timezone":  {"timezone", func(value string) { current.Timezone = value }},
		"currency":  {"currency", func(value string) { current.Currency = value }},
	}
	set := bson.M{}
	for key, raw := range patch {
		field, ok := allowed[key]
		if !ok {
			return nil, fmt.Errorf("trip field %s is not editable", key)
		}
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("trip field %s must be a string", key)
		}
		field.assign(value)
		set[field.bsonName] = value
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no editable trip fields")
	}
	if err := current.Validate(); err != nil {
		return nil, err
	}
	return set, nil
}

func (r *Repository) BumpTripRevision(ctx context.Context, tripID string) (model.Trip, error) {
	var result model.Trip
	err := r.collection("trips").FindOneAndUpdate(
		ctx,
		bson.M{"_id": tripID},
		bson.M{"$set": bson.M{"updated_at": time.Now().UTC()}, "$inc": bson.M{"version": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Trip{}, ErrNotFound
	}
	return result, err
}

func (r *Repository) ListRecords(ctx context.Context, tripID string, resource model.Resource) ([]model.Record, error) {
	if _, err := r.GetTrip(ctx, tripID); err != nil {
		return nil, err
	}
	return r.listRecords(ctx, tripID, resource)
}

func (r *Repository) ListRecordsForMember(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	memberID string,
) ([]model.Record, error) {
	if resource != model.PackingItems {
		return r.ListRecords(ctx, tripID, resource)
	}
	cursor, err := r.collection("records").Find(
		ctx,
		bson.M{
			"trip_id": tripID, "resource": resource,
			"data.ownerMemberId": memberID,
		},
		options.Find().SetSort(bson.D{{Key: "group_key", Value: 1}, {Key: "position", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]model.Record, 0)
	return rows, cursor.All(ctx, &rows)
}

func (r *Repository) GetRecordForMember(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	id string,
	memberID string,
) (model.Record, error) {
	row, err := r.GetRecord(ctx, tripID, resource, id)
	if err != nil {
		return model.Record{}, err
	}
	if resource == model.PackingItems && fmt.Sprint(row.Data["ownerMemberId"]) != memberID {
		return model.Record{}, ErrNotFound
	}
	return row, nil
}

func (r *Repository) InsertAuditLog(ctx context.Context, item model.AuditLog) error {
	item.ID = newID()
	item.CreatedAt = time.Now().UTC()
	_, err := r.collection("audit_logs").InsertOne(ctx, item)
	return err
}

func (r *Repository) ListAuditLogs(
	ctx context.Context,
	tripID, memberID string,
	limit int64,
) ([]model.AuditLog, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	cursor, err := r.collection("audit_logs").Find(
		ctx,
		bson.M{
			"trip_id": tripID,
			"$or": bson.A{
				bson.M{"resource": bson.M{"$ne": string(model.PackingItems)}},
				bson.M{"before.ownerMemberId": memberID},
				bson.M{"after.ownerMemberId": memberID},
			},
		},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]model.AuditLog, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return visibleAuditLogsForMember(rows, memberID), nil
}

func visibleAuditLogsForMember(rows []model.AuditLog, memberID string) []model.AuditLog {
	visible := make([]model.AuditLog, 0, len(rows))
	for _, row := range rows {
		if row.Resource == string(model.PackingItems) &&
			auditOwnerMemberID(row.Before) != memberID &&
			auditOwnerMemberID(row.After) != memberID {
			continue
		}
		visible = append(visible, row)
	}
	return visible
}

func auditOwnerMemberID(data map[string]any) string {
	if data == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(data["ownerMemberId"]))
}

func (r *Repository) listRecords(ctx context.Context, tripID string, resource model.Resource) ([]model.Record, error) {
	cursor, err := r.collection("records").Find(
		ctx,
		bson.M{"trip_id": tripID, "resource": resource},
		options.Find().SetSort(bson.D{{Key: "group_key", Value: 1}, {Key: "position", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rows := make([]model.Record, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *Repository) GetRecord(ctx context.Context, tripID string, resource model.Resource, id string) (model.Record, error) {
	var row model.Record
	err := r.collection("records").FindOne(ctx, bson.M{"_id": id, "trip_id": tripID, "resource": resource}).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Record{}, ErrNotFound
	}
	if err != nil {
		return model.Record{}, err
	}
	return row, nil
}

func (r *Repository) GetExpenseByBooking(
	ctx context.Context,
	tripID, bookingID string,
) (model.Record, error) {
	var row model.Record
	err := r.collection("records").FindOne(ctx, bson.M{
		"trip_id": tripID, "resource": model.Expenses,
		"data.bookingRecordId": bookingID,
	}).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.Record{}, ErrNotFound
	}
	return row, err
}

func (r *Repository) CreateRecord(ctx context.Context, row model.Record) (model.Record, error) {
	if err := validateRecordActor(row.CreatedByMemberID, "createdByMemberId"); err != nil {
		return model.Record{}, err
	}
	if err := validateRecordActor(row.UpdatedByMemberID, "updatedByMemberId"); err != nil {
		return model.Record{}, err
	}
	if _, err := r.GetTrip(ctx, row.TripID); err != nil {
		return model.Record{}, err
	}
	if err := model.ValidateRecordData(row.Resource, row.Data); err != nil {
		return model.Record{}, err
	}
	if row.Resource == model.Days {
		if err := r.validateDayDate(ctx, row.TripID, fmt.Sprint(row.Data["date"]), ""); err != nil {
			return model.Record{}, err
		}
	}
	if err := r.ValidateRecordReferences(ctx, row.TripID, row.Resource, row.Data); err != nil {
		return model.Record{}, err
	}
	if row.Resource == model.Members {
		if err := r.ValidateTripMemberSelection(
			ctx,
			row.TripID,
			"",
			strings.TrimSpace(fmt.Sprint(row.Data["memberId"])),
		); err != nil {
			return model.Record{}, err
		}
	}
	row.RecordKey = strings.TrimSpace(row.RecordKey)
	if err := model.ValidateRecordKey(row.RecordKey); err != nil {
		return model.Record{}, err
	}
	now := time.Now().UTC()
	row.GroupKey = groupKey(row.Resource, row.Data)
	row.ID, row.Version, row.CreatedAt, row.UpdatedAt = newID(), 1, now, now
	position, err := r.nextPosition(ctx, row.TripID, row.Resource, row.GroupKey)
	if err != nil {
		return model.Record{}, err
	}
	row.Position = position
	_, err = r.collection("records").InsertOne(ctx, row)
	if mongo.IsDuplicateKeyError(err) {
		return model.Record{}, fmt.Errorf("recordKey or position already exists")
	}
	return row, err
}

func (r *Repository) nextPosition(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	group string,
) (int64, error) {
	var latest struct {
		Position int64 `bson:"position"`
	}
	err := r.collection("records").FindOne(
		ctx,
		bson.M{"trip_id": tripID, "resource": resource, "group_key": group},
		options.FindOne().SetSort(bson.D{{Key: "position", Value: -1}}),
	).Decode(&latest)
	if err == nil {
		return latest.Position + 1, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	return 0, err
}

func (r *Repository) UpdateRecord(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	id string,
	version int64,
	data map[string]any,
	actorMemberID string,
) (model.Record, error) {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return model.Record{}, err
	}
	if err := model.ValidateRecordData(resource, data); err != nil {
		return model.Record{}, err
	}
	current, err := r.GetRecord(ctx, tripID, resource, id)
	if err != nil {
		return model.Record{}, err
	}
	if current.Version != version {
		return model.Record{}, ErrConflict
	}
	if resource == model.Days {
		if err := r.validateDayDate(ctx, tripID, fmt.Sprint(data["date"]), id); err != nil {
			return model.Record{}, err
		}
	}
	if err := r.ValidateRecordReferences(ctx, tripID, resource, data); err != nil {
		return model.Record{}, err
	}
	if resource == model.Members {
		if err := r.ValidateTripMemberSelection(
			ctx,
			tripID,
			id,
			strings.TrimSpace(fmt.Sprint(data["memberId"])),
		); err != nil {
			return model.Record{}, err
		}
	}
	if err := r.validateRecordUpdateDependents(ctx, tripID, resource, id, data); err != nil {
		return model.Record{}, err
	}
	targetGroup, moved := targetGroupForUpdate(current, data)
	set := bson.M{
		"data": data, "group_key": targetGroup,
		"updated_by_member_id": actorMemberID, "updated_at": time.Now().UTC(),
	}
	if moved {
		position, err := r.nextPosition(ctx, tripID, resource, targetGroup)
		if err != nil {
			return model.Record{}, err
		}
		set["position"] = position
	}
	var row model.Record
	err = r.collection("records").FindOneAndUpdate(
		ctx,
		bson.M{"_id": id, "trip_id": tripID, "resource": resource, "version": version},
		bson.M{"$set": set, "$inc": bson.M{"version": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&row)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, lookupErr := r.GetRecord(ctx, tripID, resource, id); errors.Is(lookupErr, ErrNotFound) {
			return model.Record{}, ErrNotFound
		}
		return model.Record{}, ErrConflict
	}
	return row, err
}

func validateRecordActor(memberID, field string) error {
	if err := model.ValidateRecordID(strings.TrimSpace(memberID)); err != nil {
		return fmt.Errorf("%s must reference a valid member id", field)
	}
	return nil
}

func targetGroupForUpdate(current model.Record, data map[string]any) (string, bool) {
	target := groupKey(current.Resource, data)
	return target, target != current.GroupKey
}

func (r *Repository) DeleteRecord(ctx context.Context, tripID string, resource model.Resource, id string, version int64) error {
	if err := r.validateRecordDeletion(ctx, tripID, resource, id); err != nil {
		return err
	}
	result, err := r.collection("records").DeleteOne(ctx, bson.M{
		"_id": id, "trip_id": tripID, "resource": resource, "version": version,
	})
	if err != nil {
		return err
	}
	if result.DeletedCount == 1 {
		return nil
	}
	if _, err := r.GetRecord(ctx, tripID, resource, id); errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return ErrConflict
}

func (r *Repository) DeletePlaceCascade(
	ctx context.Context,
	tripID, id string,
	version int64,
	actorMemberID string,
) error {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return err
	}
	place, err := r.GetRecord(ctx, tripID, model.Places, id)
	if err != nil {
		return err
	}
	if place.Version != version {
		return ErrConflict
	}

	days, err := r.ListRecords(ctx, tripID, model.Days)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, day := range days {
		entries := dayPlaceEntries(day.Data["placeEntries"])
		filtered := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			if fmt.Sprint(entry["placeId"]) != id {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == len(entries) {
			continue
		}
		day.Data["placeEntries"] = filtered
		if _, err := r.collection("records").UpdateOne(
			ctx,
			bson.M{"_id": day.ID, "version": day.Version},
			bson.M{
				"$set": bson.M{
					"data": day.Data, "updated_at": now,
					"updated_by_member_id": actorMemberID,
				},
				"$inc": bson.M{"version": 1},
			},
		); err != nil {
			return err
		}
	}
	if _, err := r.collection("records").UpdateMany(
		ctx,
		bson.M{"trip_id": tripID, "resource": model.Bookings, "data.placeId": id},
		bson.M{
			"$unset": bson.M{"data.placeId": ""},
			"$set": bson.M{
				"updated_at": now, "updated_by_member_id": actorMemberID,
			},
			"$inc": bson.M{"version": 1},
		},
	); err != nil {
		return err
	}
	for _, resource := range []model.Resource{model.ResearchEvidence} {
		if _, err := r.collection("records").UpdateMany(
			ctx,
			bson.M{"trip_id": tripID, "resource": resource, "data.placeId": id},
			bson.M{
				"$unset": bson.M{"data.placeId": ""},
				"$set": bson.M{
					"updated_at": now, "updated_by_member_id": actorMemberID,
				},
				"$inc": bson.M{"version": 1},
			},
		); err != nil {
			return err
		}
	}
	if _, err := r.collection("records").DeleteMany(ctx, bson.M{
		"trip_id": tripID, "resource": model.Segments,
		"$or": bson.A{
			bson.M{"data.fromPlaceId": id},
			bson.M{"data.toPlaceId": id},
		},
	}); err != nil {
		return err
	}
	result, err := r.collection("records").DeleteOne(ctx, bson.M{
		"_id": id, "trip_id": tripID, "resource": model.Places, "version": version,
	})
	if err != nil {
		return err
	}
	if result.DeletedCount != 1 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) ValidateRecordReferences(ctx context.Context, tripID string, resource model.Resource, data map[string]any) error {
	if resource == model.Members {
		memberID := strings.TrimSpace(fmt.Sprint(data["memberId"]))
		if _, err := r.GetLibraryRecord(ctx, model.LibraryMembers, memberID); err != nil {
			return fmt.Errorf("memberId does not reference a global member: %w", err)
		}
	}
	if resource == model.Places {
		typeID := strings.TrimSpace(fmt.Sprint(data["type"]))
		tag, err := r.GetLibraryRecord(ctx, model.LibraryTags, typeID)
		if err != nil || fmt.Sprint(tag.Data["category"]) != "place-type" {
			return fmt.Errorf("地点 type 必须引用一个有效的地点类型标签")
		}
	}
	if resource == model.Expenses {
		typeID := strings.TrimSpace(fmt.Sprint(data["type"]))
		tag, err := r.GetLibraryRecord(ctx, model.LibraryTags, typeID)
		if err != nil || fmt.Sprint(tag.Data["category"]) != "expense-type" {
			return fmt.Errorf("费用 type 必须引用一个有效的费用类型标签")
		}
	}
	if resource == model.Bookings {
		typeID := strings.TrimSpace(fmt.Sprint(data["type"]))
		tag, err := r.GetLibraryRecord(ctx, model.LibraryTags, typeID)
		if err != nil || fmt.Sprint(tag.Data["category"]) != model.TagCategoryExpenseType {
			return fmt.Errorf("预订 type 必须引用一个有效的费用类型标签")
		}
	}
	if resource == model.Bookings || resource == model.ResearchEvidence {
		platformID := strings.TrimSpace(fmt.Sprint(data["platformId"]))
		if platformID != "" && platformID != "<nil>" {
			tag, err := r.GetLibraryRecord(ctx, model.LibraryTags, platformID)
			if err != nil || fmt.Sprint(tag.Data["category"]) != model.TagCategoryPlatformType {
				return fmt.Errorf("platformId 必须引用一个有效的平台类型标签")
			}
		}
	}
	if resource == model.Days {
		for _, placeID := range dayPlaceIDs(data) {
			if _, err := r.GetRecord(ctx, tripID, model.Places, placeID); err != nil {
				return fmt.Errorf("日程地点不存在或不属于当前旅行: %w", err)
			}
		}
	}
	for _, field := range []string{
		"ownerMemberId", "payerMemberId", "bookerMemberId", "recordedByMemberId",
	} {
		memberID := strings.TrimSpace(fmt.Sprint(data[field]))
		if memberID != "" && memberID != "<nil>" {
			if err := r.validateSelectedMember(ctx, tripID, memberID); err != nil {
				return fmt.Errorf("%s 不是当前旅行成员: %w", field, err)
			}
		}
	}
	for _, memberID := range stringValues(data["splitMemberIds"]) {
		if err := r.validateSelectedMember(ctx, tripID, memberID); err != nil {
			return fmt.Errorf("分摊成员不属于当前旅行: %w", err)
		}
	}
	for _, reference := range recordReferences(resource, data) {
		for _, id := range reference.IDs {
			if _, err := r.GetRecord(ctx, tripID, reference.Target, id); err != nil {
				return fmt.Errorf(
					"%s does not reference a %s record in this trip: %w",
					reference.Field,
					reference.Target,
					err,
				)
			}
		}
	}
	if resource == model.Segments {
		dayID := strings.TrimSpace(fmt.Sprint(data["dayId"]))
		day, err := r.GetRecord(ctx, tripID, model.Days, dayID)
		if err != nil {
			return err
		}
		places := map[string]struct{}{}
		for _, placeID := range dayPlaceIDs(day.Data) {
			places[placeID] = struct{}{}
		}
		for _, field := range []string{"fromPlaceId", "toPlaceId"} {
			if _, exists := places[strings.TrimSpace(fmt.Sprint(data[field]))]; !exists {
				return fmt.Errorf("%s 必须是该执行日已经关联的地点", field)
			}
		}
	}
	return nil
}

func (r *Repository) ValidateTripMemberSelection(
	ctx context.Context,
	tripID, excludeID, memberID string,
) error {
	filter := bson.M{
		"trip_id":       tripID,
		"resource":      model.Members,
		"data.memberId": memberID,
	}
	if excludeID != "" {
		filter["_id"] = bson.M{"$ne": excludeID}
	}
	count, err := r.collection("records").CountDocuments(
		ctx,
		filter,
		options.Count().SetLimit(1),
	)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("该成员已经加入当前旅行")
	}
	return nil
}

func (r *Repository) validateDayDate(ctx context.Context, tripID, date, excludeID string) error {
	trip, err := r.GetTrip(ctx, tripID)
	if err != nil {
		return err
	}
	if date < trip.StartDate || date > trip.EndDate {
		return fmt.Errorf(
			"执行日必须位于旅行日期范围 %s 至 %s 内",
			trip.StartDate,
			trip.EndDate,
		)
	}
	filter := bson.M{
		"trip_id":   tripID,
		"resource":  model.Days,
		"data.date": date,
	}
	if excludeID != "" {
		filter["_id"] = bson.M{"$ne": excludeID}
	}
	count, err := r.collection("records").CountDocuments(ctx, filter, options.Count().SetLimit(1))
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("当前旅行中已经存在 %s 的执行日", date)
	}
	return nil
}

func (r *Repository) validateRecordUpdateDependents(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	id string,
	data map[string]any,
) error {
	if resource == model.Days {
		allowed := map[string]struct{}{}
		for _, placeID := range dayPlaceIDs(data) {
			allowed[placeID] = struct{}{}
		}
		segments, err := r.listRecords(ctx, tripID, model.Segments)
		if err != nil {
			return err
		}
		for _, segment := range segments {
			if fmt.Sprint(segment.Data["dayId"]) != id {
				continue
			}
			if _, ok := allowed[fmt.Sprint(segment.Data["fromPlaceId"])]; !ok {
				return fmt.Errorf("请先删除以已移除地点为起点的交通段")
			}
			if _, ok := allowed[fmt.Sprint(segment.Data["toPlaceId"])]; !ok {
				return fmt.Errorf("请先删除以已移除地点为终点的交通段")
			}
		}
	}
	return nil
}

func (r *Repository) validateRecordDeletion(ctx context.Context, tripID string, resource model.Resource, id string) error {
	var filters []bson.M
	switch resource {
	case model.Days:
		filters = []bson.M{
			{"trip_id": tripID, "resource": model.Segments, "data.dayId": id},
			{"trip_id": tripID, "resource": model.PackingItems, "data.dayIds": id},
			{"trip_id": tripID, "resource": model.ResearchEvidence, "data.dayIds": id},
		}
	case model.Places:
		filters = []bson.M{
			{"trip_id": tripID, "resource": model.Days, "data.placeEntries.placeId": id},
			{"trip_id": tripID, "resource": model.Bookings, "data.placeId": id},
			{"trip_id": tripID, "resource": model.ResearchEvidence, "data.placeId": id},
		}
	case model.Members:
		impact, err := r.GetTripMemberDeletionImpact(ctx, tripID, id)
		if err != nil {
			return err
		}
		if len(impact.References) > 0 ||
			len(impact.CascadeExpenses) > 0 ||
			impact.HasPrivatePackingItems {
			return fmt.Errorf(
				"%s: %w",
				tripMemberDeletionMessage(impact),
				ErrReferenced,
			)
		}
		return nil
	}
	for _, filter := range filters {
		count, err := r.collection("records").CountDocuments(ctx, filter, options.Count().SetLimit(1))
		if err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("资料仍被现有行程引用: %w", ErrReferenced)
		}
	}
	return nil
}

func (r *Repository) Reorder(
	ctx context.Context,
	tripID string,
	resource model.Resource,
	group string,
	ids []string,
	actorMemberID string,
) error {
	if err := validateRecordActor(actorMemberID, "updatedByMemberId"); err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("ids must not be empty")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("ids must not contain empty values")
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("ids must not contain duplicates")
		}
		seen[id] = struct{}{}
	}
	filter := bson.M{"trip_id": tripID, "resource": resource, "group_key": group}
	total, err := r.collection("records").CountDocuments(ctx, filter)
	if err != nil {
		return err
	}
	if total != int64(len(ids)) {
		return fmt.Errorf("ids must be the complete group ordering")
	}
	matched, err := r.collection("records").CountDocuments(ctx, bson.M{
		"trip_id": tripID, "resource": resource, "group_key": group, "_id": bson.M{"$in": ids},
	})
	if err != nil {
		return err
	}
	if matched != total {
		return ErrNotFound
	}
	var highest struct {
		Position int64 `bson:"position"`
	}
	if err := r.collection("records").FindOne(
		ctx,
		filter,
		options.FindOne().SetSort(bson.D{{Key: "position", Value: -1}}),
	).Decode(&highest); err != nil {
		return err
	}
	if highest.Position > math.MaxInt64-int64(len(ids)) {
		return fmt.Errorf("record positions are too large to reorder")
	}
	temporaryBase := highest.Position + 1
	for offset, id := range ids {
		if _, err := r.collection("records").UpdateOne(
			ctx,
			bson.M{"_id": id, "trip_id": tripID, "resource": resource, "group_key": group},
			bson.M{"$set": bson.M{"position": temporaryBase + int64(offset)}},
		); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	for position, id := range ids {
		if _, err := r.collection("records").UpdateOne(
			ctx,
			bson.M{"_id": id, "trip_id": tripID, "resource": resource, "group_key": group},
			bson.M{
				"$set": bson.M{
					"position": int64(position), "updated_by_member_id": actorMemberID,
					"updated_at": now,
				},
				"$inc": bson.M{"version": 1},
			},
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) GetIdempotencyRequest(
	ctx context.Context,
	scope, key, requestHash string,
) (model.IdempotencyRequest, error) {
	var request model.IdempotencyRequest
	err := r.collection("idempotency_requests").FindOne(ctx, bson.M{
		"scope": scope, "key": key, "expires_at": bson.M{"$gt": time.Now().UTC()},
	}).Decode(&request)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return model.IdempotencyRequest{}, ErrNotFound
	}
	if err != nil {
		return model.IdempotencyRequest{}, err
	}
	if request.RequestHash != requestHash {
		return model.IdempotencyRequest{}, ErrIdempotencyConflict
	}
	return request, nil
}

func (r *Repository) StoreIdempotencyRequest(
	ctx context.Context,
	scope, key, requestHash string,
	response []byte,
) error {
	now := time.Now().UTC()
	request := model.IdempotencyRequest{
		ID: newID(), Scope: scope, Key: key, RequestHash: requestHash, Response: response,
		CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	_, err := r.collection("idempotency_requests").InsertOne(ctx, request)
	if mongo.IsDuplicateKeyError(err) {
		return ErrIdempotencyConflict
	}
	return err
}

func groupKey(resource model.Resource, data map[string]any) string {
	switch resource {
	case model.Segments:
		return strings.TrimSpace(fmt.Sprint(data["dayId"]))
	default:
		return ""
	}
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("secure id generation failed: %v", err))
	}
	return hex.EncodeToString(bytes)
}
