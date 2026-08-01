// 真实 MongoDB 集成测试验证 validator、索引、指纹和副本集事务边界。
package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/model"
)

func integrationRepository(t *testing.T) *Repository {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI 未设置，跳过真实 MongoDB 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("my_travel_contract_%d", time.Now().UnixNano())
	repo := &Repository{client: client, db: client.Database(databaseName)}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = repo.db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return repo
}

func TestMongoIntegrationInitializeInstallsCurrentContract(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize must be idempotent: %v", err)
	}

	fingerprint, err := repo.DatabaseContractFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint != CurrentDatabaseContractFingerprint() {
		t.Fatalf("stored fingerprint=%q", fingerprint)
	}

	for collection, expected := range map[string][]string{
		"snapshots":         {"trip_created_at", "mirror_state_created_at"},
		"library_snapshots": {"created_at", "mirror_state_created_at"},
	} {
		cursor, err := repo.collection(collection).Indexes().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var indexes []bson.M
		if err := cursor.All(ctx, &indexes); err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, index := range indexes {
			found[fmt.Sprint(index["name"])] = true
		}
		for _, name := range expected {
			if !found[name] {
				t.Fatalf("%s index %s was not installed: %#v", collection, name, found)
			}
		}
	}
}

func TestMongoIntegrationValidatorRejectsUnknownRecordData(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	invalid := model.Record{
		ID: "record-invalid", TripID: "trip-a", Resource: model.Places,
		RecordKey: "place-invalid", GroupKey: "", Position: 0, Version: 1,
		Data: map[string]any{
			"name": "测试地点", "unexpectedField": "不允许",
		},
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := repo.collection("records").InsertOne(ctx, invalid); err == nil {
		t.Fatal("MongoDB validator accepted an unknown current-model field")
	}
}

func TestMongoIntegrationValidatorsAcceptEveryCurrentDocumentFamily(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	records := []any{
		model.Record{
			ID: "record-day", TripID: "trip-current", Resource: model.Days,
			RecordKey: "day-1", GroupKey: "", Position: 0, Version: 1,
			Data: map[string]any{
				"title": "第一天", "date": "2026-09-01",
				"placeEntries": []any{
					map[string]any{"placeId": "place-1", "arrivalTime": "09:00"},
				},
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-place", TripID: "trip-current", Resource: model.Places,
			RecordKey: "place-1", GroupKey: "", Position: 0, Version: 1,
			Data: map[string]any{
				"name": "测试地点", "latitude": 30.5, "longitude": 104.1,
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-segment", TripID: "trip-current", Resource: model.Segments,
			RecordKey: "segment-1", GroupKey: "day-1", Position: 0, Version: 1,
			Data: map[string]any{
				"dayId": "day-1", "fromPlaceId": "place-1",
				"toPlaceId": "place-2", "mode": "drive",
				"durationMinutes": int64(30), "distanceMeters": int64(15000),
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-booking", TripID: "trip-current", Resource: model.Bookings,
			RecordKey: "booking-1", GroupKey: "", Position: 0, Version: 1,
			Data: map[string]any{
				"item": "住宿", "placeId": "place-1", "status": "confirmed",
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-expense", TripID: "trip-current", Resource: model.Expenses,
			RecordKey: "expense-1", GroupKey: "", Position: 0, Version: 1,
			Data: map[string]any{
				"item": "住宿", "type": "hotel", "amountMinor": int64(12000),
				"splitMode": "selected", "splitMemberIds": []string{"member-1"},
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-packing", TripID: "trip-current", Resource: model.PackingItems,
			RecordKey: "packing-1", GroupKey: "", Position: 0, Version: 1,
			Data: map[string]any{
				"item": "证件", "quantity": int64(1), "packed": true,
				"dayIds": []string{"day-1"}, "ownerMemberId": "member-1",
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-research", TripID: "trip-current", Resource: model.ResearchEvidence,
			RecordKey: "research-1", GroupKey: "", Position: 0, Version: 1,
			Data: map[string]any{
				"title": "开放时间", "content": "开放时间核验内容",
				"platformId": "tag-platform", "recordedByMemberId": "member-1",
				"dayIds": []string{"day-1"},
			},
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "record-member", TripID: "trip-current", Resource: model.Members,
			RecordKey: "member-1", GroupKey: "", Position: 0, Version: 1,
			Data:      map[string]any{"memberId": "member-1", "role": "owner"},
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for index, value := range records {
		row := value.(model.Record)
		row.CreatedByMemberID = "member-1"
		row.UpdatedByMemberID = "member-1"
		records[index] = row
	}
	hashA, hashB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	documents := map[string][]any{
		"trips": {
			model.Trip{
				ID: "trip-current", Title: "当前模型测试",
				StartDate: "2026-09-01", EndDate: "2026-09-02",
				Timezone: "Asia/Shanghai", Currency: "CNY", Version: 1,
				CreatedAt: now, UpdatedAt: now,
			},
		},
		"records": records,
		"library_records": {
			model.LibraryRecord{
				ID: "member-1", Resource: model.LibraryMembers, RecordKey: "member-1",
				Position: 0, Version: 1,
				Data: map[string]any{
					"displayName": "测试成员", "mobile": "13800138000", "admin": true,
				},
				CreatedAt: now, UpdatedAt: now,
			},
			model.LibraryRecord{
				ID: "tag-1", Resource: model.LibraryTags, RecordKey: "tag-1",
				Position: 0, Version: 1,
				Data: map[string]any{
					"name": "住宿", "category": "expense-type", "color": "#123456",
				},
				CreatedAt: now, UpdatedAt: now,
			},
			model.LibraryRecord{
				ID: "tag-platform", Resource: model.LibraryTags,
				RecordKey: "platform-amap", Position: 1, Version: 1,
				Data: map[string]any{
					"name": "高德", "category": "platform-type", "color": "#1677FF",
				},
				CreatedAt: now, UpdatedAt: now,
			},
		},
		"member_credentials": {
			model.MemberCredential{MemberID: "member-1", PasswordHash: "hash"},
		},
		"memberships": {
			model.Membership{
				ID: "membership-1", TripID: "trip-current", MemberID: "member-1",
				Role: "owner", CreatedAt: now,
			},
		},
		"sessions": {
			model.Session{
				ID: "session-1", MemberID: "member-1", CSRFToken: "csrf",
				ExpiresAt: now.Add(time.Hour),
			},
		},
		"audit_logs": {
			model.AuditLog{
				ID: "audit-1", TripID: "trip-current", MemberID: "member-1",
				Module: "trip", Action: "create", Resource: "trips",
				RecordID: "trip-current", After: map[string]any{"title": "当前模型测试"},
				CreatedAt: now,
			},
		},
		"snapshots": {
			model.Snapshot{
				ID: "snapshot-1", TripID: "trip-current", Revision: 1,
				Reason: "contract-test", Payload: []byte("{}"),
				SHA256: hashA, ContentChecksum: hashB,
				MirrorState: "pending", CreatedAt: now,
			},
		},
		"library_snapshots": {
			model.LibrarySnapshot{
				ID: "library-snapshot-1", Revision: 1, Reason: "contract-test",
				Payload: []byte("{}"), SHA256: hashA, ContentChecksum: hashB,
				MirrorState: "pending", CreatedAt: now,
			},
		},
		"idempotency_requests": {
			model.IdempotencyRequest{
				ID: "idempotency-1", Scope: "test", Key: "request-1",
				RequestHash: hashA, Response: []byte("{}"),
				CreatedAt: now, ExpiresAt: now.Add(time.Hour),
			},
		},
	}
	for collection, rows := range documents {
		if _, err := repo.collection(collection).InsertMany(ctx, rows); err != nil {
			t.Fatalf("%s rejected a current document family: %v", collection, err)
		}
	}
}

func TestMongoIntegrationTransactionRollsBackAggregateWrite(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sentinel := errors.New("force rollback")
	err := repo.WithTransaction(ctx, func(tx context.Context) error {
		_, insertErr := repo.collection("trips").InsertOne(tx, model.Trip{
			ID: "rollback-trip", Title: "回滚测试",
			StartDate: "2026-09-01", EndDate: "2026-09-02",
			Timezone: "Asia/Shanghai", Currency: "CNY", Version: 1,
			CreatedAt: now, UpdatedAt: now,
		})
		if insertErr != nil {
			return insertErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction error=%v", err)
	}
	count, err := repo.collection("trips").CountDocuments(
		ctx,
		bson.D{{Key: "_id", Value: "rollback-trip"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("transaction left a partially committed Trip")
	}
}

func TestMongoIntegrationReorderWorksWithStrictValidator(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	trip := model.Trip{
		ID: "reorder-trip", Title: "排序测试",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
		Timezone: "Asia/Shanghai", Currency: "CNY", Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := repo.collection("trips").InsertOne(ctx, trip); err != nil {
		t.Fatal(err)
	}
	records := []any{
		model.Record{
			ID: "day-a", TripID: trip.ID, Resource: model.Days,
			RecordKey: "day-a", GroupKey: "", Position: 0, Version: 1,
			Data:              map[string]any{"title": "第一天", "date": "2026-09-01"},
			CreatedByMemberID: "member-owner", UpdatedByMemberID: "member-owner",
			CreatedAt: now, UpdatedAt: now,
		},
		model.Record{
			ID: "day-b", TripID: trip.ID, Resource: model.Days,
			RecordKey: "day-b", GroupKey: "", Position: 1, Version: 1,
			Data:              map[string]any{"title": "第二天", "date": "2026-09-02"},
			CreatedByMemberID: "member-owner", UpdatedByMemberID: "member-owner",
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if _, err := repo.collection("records").InsertMany(ctx, records); err != nil {
		t.Fatal(err)
	}
	if err := repo.WithTransaction(ctx, func(tx context.Context) error {
		return repo.Reorder(
			tx,
			trip.ID,
			model.Days,
			"",
			[]string{"day-b", "day-a"},
			"member-owner",
		)
	}); err != nil {
		t.Fatalf("strict validator rejected reorder: %v", err)
	}
	rows, err := repo.ListRecords(ctx, trip.ID, model.Days)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "day-b" || rows[1].ID != "day-a" {
		t.Fatalf("reordered rows=%#v", rows)
	}
	for position, row := range rows {
		if row.Position != int64(position) || row.Version != 2 ||
			row.UpdatedByMemberID != "member-owner" {
			t.Fatalf("row %s was not finalized correctly: %#v", row.ID, row)
		}
	}
}

func TestMongoIntegrationInitializationRejectsInvalidShape(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if _, err := repo.collection("records").InsertOne(ctx, bson.D{
		{Key: "_id", Value: "invalid-record"},
		{Key: "unexpected_field", Value: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Initialize(ctx); err == nil ||
		!strings.Contains(err.Error(), "outside the current model") {
		t.Fatalf("invalid document did not stop initialization: %v", err)
	}
	count, err := repo.collection("records").CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("Initialize silently changed invalid data, count=%d", count)
	}
	names, err := repo.db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "records" {
		t.Fatalf("Initialize partially installed the contract before rejection: %v", names)
	}
}

func TestMongoIntegrationRecordValidatorRequiresAuditActors(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err := repo.collection("records").InsertOne(ctx, model.Record{
		ID: "record-without-actors", TripID: "trip-current", Resource: model.Days,
		RecordKey: "day-a", GroupKey: "", Position: 0, Version: 1,
		Data:      map[string]any{"title": "第一天", "date": "2026-09-01"},
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Fatal("record without createdByMemberId and updatedByMemberId was accepted")
	}
	_, err = repo.collection("records").InsertOne(ctx, model.Record{
		ID: "record-invalid-actor", TripID: "trip-current", Resource: model.Days,
		RecordKey: "day-a", GroupKey: "", Position: 0, Version: 1,
		Data:              map[string]any{"title": "第一天", "date": "2026-09-01"},
		CreatedByMemberID: "member..owner", UpdatedByMemberID: "member-owner",
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Fatal("record actor containing a traversal segment was accepted")
	}
}

func TestMongoIntegrationInitializationRejectsUnmanagedIndex(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if _, err := repo.collection("trips").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "title", Value: 1}},
		Options: options.Index().SetName("unmanaged_title"),
	}); err != nil {
		t.Fatal(err)
	}
	err := repo.Initialize(ctx)
	if err == nil || !strings.Contains(err.Error(), "unmanaged index unmanaged_title") {
		t.Fatalf("unmanaged index did not stop initialization: %v", err)
	}
	if _, err := repo.DatabaseContractFingerprint(ctx); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("failed contract installation persisted a fingerprint: %v", err)
	}
}

func TestMongoIntegrationInitializationRejectsWrongManagedIndexName(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	zero := int32(0)
	if _, err := repo.collection("sessions").Indexes().CreateOne(
		ctx,
		mongo.IndexModel{
			Keys: bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().
				SetName("unexpected_session_ttl").
				SetExpireAfterSeconds(zero),
		},
	); err != nil {
		t.Fatal(err)
	}

	err := repo.Initialize(ctx)
	if err == nil || !strings.Contains(err.Error(), "ensure indexes for sessions") {
		t.Fatalf("wrong managed index name did not stop initialization: %v", err)
	}
	if _, err := repo.DatabaseContractFingerprint(ctx); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("failed contract installation persisted a fingerprint: %v", err)
	}
}
