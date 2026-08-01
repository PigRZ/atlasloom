package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"my-travel/server/internal/model"
)

func TestSameRecordMeaningTreatsNestedBSONAndJSONAsEquivalent(t *testing.T) {
	fromMongo := model.Record{
		Position: 1,
		Data: map[string]any{
			"title": "第一天",
			"date":  "2026-07-01",
			"placeEntries": bson.A{
				bson.D{
					{Key: "placeId", Value: "place-a"},
					{Key: "arrivalTime", Value: "09:00"},
					{Key: "additionalInfo", Value: "从北门进入"},
				},
			},
		},
	}
	fromSnapshot := model.Record{
		Position: 1,
		Data: map[string]any{
			"date":  "2026-07-01",
			"title": "第一天",
			"placeEntries": []any{
				map[string]any{
					"additionalInfo": "从北门进入",
					"arrivalTime":    "09:00",
					"placeId":        "place-a",
				},
			},
		},
	}

	if !sameRecordMeaning(fromMongo, fromSnapshot) {
		t.Fatal("相同的 BSON 与 JSON 地点安排被误判为 Day 更新")
	}
	fromSnapshot.Data["placeEntries"].([]any)[0].(map[string]any)["arrivalTime"] = "09:30"
	if sameRecordMeaning(fromMongo, fromSnapshot) {
		t.Fatal("真实的地点安排时间变化被错误忽略")
	}
}

func TestValidateSnapshotPayloadAcceptsDirectDayPlaceModel(t *testing.T) {
	payload := validSnapshotPayload(t)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateSnapshotPayload(raw, payload.Trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if validated.ContentChecksum != payload.ContentChecksum {
		t.Fatal("content checksum changed during validation")
	}
}

func TestValidateSnapshotPayloadAcceptsDeletedTripForBackupVerification(t *testing.T) {
	payload := validSnapshotPayload(t)
	deletedAt := payload.ExportedAt.Add(time.Hour)
	payload.Trip.DeletedAt = &deletedAt
	payload.Trip.DeletedByMemberID = "member-owner"
	raw := snapshotPayloadJSON(t, &payload)

	validated, err := ValidateSnapshotPayload(raw, payload.Trip.ID)
	if err != nil {
		t.Fatalf("deleted trip snapshot cannot be verified as backup data: %v", err)
	}
	if validated.Trip.DeletedAt == nil ||
		validated.Trip.DeletedByMemberID != "member-owner" {
		t.Fatalf("deleted trip metadata was not preserved: %#v", validated.Trip)
	}
}

func TestValidateSnapshotPayloadRejectsTamperingAndMissingResource(t *testing.T) {
	payload := validSnapshotPayload(t)
	payload.Datasets["days"][0].Data["title"] = "tampered"
	raw, _ := json.Marshal(payload)
	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "contentChecksum") {
		t.Fatalf("tampering was not rejected: %v", err)
	}

	payload = validSnapshotPayload(t)
	delete(payload.Datasets, "members")
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()
	raw, _ = json.Marshal(payload)
	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "missing resource") {
		t.Fatalf("missing resource was not rejected: %v", err)
	}
}

func TestValidateSnapshotPayloadRejectsBrokenDayPlaceReference(t *testing.T) {
	payload := validSnapshotPayload(t)
	payload.Datasets["days"][0].Data["placeEntries"] = []any{
		map[string]any{"placeId": "missing-place"},
	}
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()
	raw, _ := json.Marshal(payload)
	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "missing places") {
		t.Fatalf("broken reference was not rejected: %v", err)
	}
}

func TestValidateSnapshotPayloadRequiresRecordActors(t *testing.T) {
	payload := validSnapshotPayload(t)
	payload.Datasets["places"][0].CreatedByMemberID = ""
	raw := snapshotPayloadJSON(t, &payload)

	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "createdByMemberId is required") {
		t.Fatalf("record without a creator was accepted: %v", err)
	}
}

func TestValidateSnapshotPayloadRejectsSegmentOutsideDay(t *testing.T) {
	payload := validSnapshotPayload(t)
	payload.Datasets["segments"][0].Data["toPlaceId"] = "place-c"
	payload.Datasets["places"] = append(payload.Datasets["places"], model.Record{
		ID: "place-c", TripID: payload.Trip.ID, Resource: model.Places,
		RecordKey: "place-c", Position: 2, Version: 1,
		Data:              map[string]any{"name": "Other"},
		CreatedByMemberID: "member-owner", UpdatedByMemberID: "member-owner",
		CreatedAt: payload.ExportedAt, UpdatedAt: payload.ExportedAt,
	})
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()
	raw, _ := json.Marshal(payload)
	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "outside its day") {
		t.Fatalf("cross-day segment was not rejected: %v", err)
	}
}

func TestValidateSnapshotPayloadRejectsUnknownEnvelopeField(t *testing.T) {
	payload := validSnapshotPayload(t)
	raw, _ := json.Marshal(payload)
	raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"unexpectedEnvelopeField":99}`)
	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown envelope field was not rejected: %v", err)
	}
}

func TestValidateSnapshotPayloadRequiresSelectedPackingOwner(t *testing.T) {
	payload := validSnapshotPayload(t)
	payload.Datasets[string(model.Members)] = []model.Record{
		snapshotTestRecord(
			payload,
			"trip-member-a",
			model.Members,
			0,
			map[string]any{"memberId": "member-a", "role": "同行成员"},
		),
	}
	payload.Datasets[string(model.PackingItems)] = []model.Record{
		snapshotTestRecord(
			payload,
			"packing-a",
			model.PackingItems,
			0,
			map[string]any{"item": "证件", "ownerMemberId": "member-a"},
		),
	}
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err != nil {
		t.Fatalf("selected packing owner was rejected: %v", err)
	}

	payload.Datasets[string(model.PackingItems)][0].Data["ownerMemberId"] = "member-b"
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err == nil || !strings.Contains(err.Error(), "不是当前旅行成员") {
		t.Fatalf("unselected packing owner was accepted: %v", err)
	}
}

func TestValidateSnapshotPayloadRequiresOneMatchingExpensePerBooking(t *testing.T) {
	payload := validSnapshotPayload(t)
	booking := snapshotTestRecord(
		payload,
		"booking-a",
		model.Bookings,
		0,
		map[string]any{
			"item": "酒店", "type": "expense-type-other",
			"bookerMemberId": "member-a",
		},
	)
	payload.Datasets[string(model.Bookings)] = []model.Record{booking}

	raw := snapshotPayloadJSON(t, &payload)
	if _, err := ValidateSnapshotPayload(raw, payload.Trip.ID); err == nil ||
		!strings.Contains(err.Error(), "必须且只能关联一条自动费用") {
		t.Fatalf("booking without its generated expense was accepted: %v", err)
	}

	expense := snapshotTestRecord(
		payload,
		"expense-a",
		model.Expenses,
		0,
		map[string]any{
			"item": "酒店", "type": "expense-type-other",
			"payerMemberId": "member-a",
			"splitMode":     "all", "splitMemberIds": []string{"member-a"},
			"bookingRecordId": booking.ID,
		},
	)
	payload.Datasets[string(model.Expenses)] = []model.Record{expense}
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err != nil {
		t.Fatalf("matching booking expense was rejected: %v", err)
	}

	duplicate := expense
	duplicate.ID = "expense-b"
	duplicate.RecordKey = duplicate.ID
	duplicate.Position = 1
	payload.Datasets[string(model.Expenses)] = append(
		payload.Datasets[string(model.Expenses)],
		duplicate,
	)
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err == nil || !strings.Contains(err.Error(), "关联了多条自动费用") {
		t.Fatalf("duplicate generated expenses were accepted: %v", err)
	}

	payload.Datasets[string(model.Expenses)] = []model.Record{expense}
	payload.Datasets[string(model.Expenses)][0].Data["item"] = "另一个项目"
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err == nil || !strings.Contains(err.Error(), "自动费用项目必须与预订一致") {
		t.Fatalf("divergent booking expense item was accepted: %v", err)
	}

	payload.Datasets[string(model.Expenses)][0].Data["item"] = "酒店"
	payload.Datasets[string(model.Expenses)][0].Data["type"] = "expense-type-another"
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err == nil || !strings.Contains(err.Error(), "自动费用类型必须与预订一致") {
		t.Fatalf("divergent booking expense type was accepted: %v", err)
	}

	payload.Datasets[string(model.Expenses)][0].Data["type"] = "expense-type-other"
	payload.Datasets[string(model.Expenses)][0].Data["payerMemberId"] = "member-b"
	if _, err := ValidateSnapshotPayload(
		snapshotPayloadJSON(t, &payload),
		payload.Trip.ID,
	); err == nil || !strings.Contains(err.Error(), "自动费用支付人必须与预订人一致") {
		t.Fatalf("divergent booking expense payer was accepted: %v", err)
	}
}

func TestMongoIntegrationSnapshotExternalReferencesUseCurrentLibrary(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	libraryIDs := make(map[string]string)
	for _, row := range []model.LibraryRecord{
		{
			Resource: model.LibraryMembers, RecordKey: "member-a",
			Data: map[string]any{"displayName": "成员 A", "status": "active"},
		},
		{
			Resource: model.LibraryMembers, RecordKey: "member-actor",
			Data: map[string]any{"displayName": "历史操作者", "status": "active"},
		},
		{
			Resource: model.LibraryTags, RecordKey: "place-type-hotel",
			Data: map[string]any{
				"name": "住宿", "category": model.TagCategoryPlaceType,
			},
		},
		{
			Resource: model.LibraryTags, RecordKey: "expense-type-other",
			Data: map[string]any{
				"name": "其他", "category": model.TagCategoryExpenseType,
			},
		},
		{
			Resource: model.LibraryTags, RecordKey: "platform-official",
			Data: map[string]any{
				"name": "官方", "category": model.TagCategoryPlatformType,
			},
		},
	} {
		created, err := repo.CreateLibraryRecord(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		libraryIDs[row.RecordKey] = created.ID
	}
	memberID := libraryIDs["member-a"]
	actorMemberID := libraryIDs["member-actor"]
	placeTypeID := libraryIDs["place-type-hotel"]
	expenseTypeID := libraryIDs["expense-type-other"]
	platformID := libraryIDs["platform-official"]

	payload := validSnapshotPayload(t)
	payload.Datasets[string(model.Members)] = []model.Record{
		snapshotTestRecord(
			payload,
			"trip-member-a",
			model.Members,
			0,
			map[string]any{"memberId": memberID, "role": "仅作旅行说明"},
		),
	}
	for index := range payload.Datasets[string(model.Places)] {
		payload.Datasets[string(model.Places)][index].Data["type"] = placeTypeID
	}
	booking := snapshotTestRecord(
		payload,
		"booking-a",
		model.Bookings,
		0,
		map[string]any{
			"item": "酒店", "type": expenseTypeID,
			"bookerMemberId": memberID, "platformId": platformID,
		},
	)
	payload.Datasets[string(model.Bookings)] = []model.Record{booking}
	payload.Datasets[string(model.Expenses)] = []model.Record{
		snapshotTestRecord(
			payload,
			"expense-a",
			model.Expenses,
			0,
			map[string]any{
				"item": "酒店", "type": expenseTypeID,
				"payerMemberId": memberID, "splitMode": "all",
				"splitMemberIds":  []string{memberID},
				"bookingRecordId": booking.ID,
			},
		),
	}
	for resource, rows := range payload.Datasets {
		for index := range rows {
			rows[index].CreatedByMemberID = actorMemberID
			rows[index].UpdatedByMemberID = actorMemberID
		}
		payload.Datasets[resource] = rows
	}
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()
	if err := repo.ValidateSnapshotExternalReferences(ctx, payload); err != nil {
		t.Fatalf("valid repository-backed references were rejected: %v", err)
	}

	payload.Datasets[string(model.Places)][0].Data["type"] = expenseTypeID
	if err := repo.ValidateSnapshotExternalReferences(ctx, payload); err == nil ||
		!strings.Contains(err.Error(), "地点类型标签") {
		t.Fatalf("wrong tag category was accepted: %v", err)
	}
	payload.Datasets[string(model.Places)][0].Data["type"] = placeTypeID

	payload.Datasets[string(model.Expenses)][0].Data["payerMemberId"] = "member-not-selected"
	if err := repo.ValidateSnapshotExternalReferences(ctx, payload); err == nil ||
		!strings.Contains(err.Error(), "不是当前旅行成员") {
		t.Fatalf("unselected member reference was accepted: %v", err)
	}
	payload.Datasets[string(model.Expenses)][0].Data["payerMemberId"] = memberID

	payload.Datasets[string(model.Members)][0].Data["memberId"] = "member-missing"
	payload.Datasets[string(model.Bookings)][0].Data["bookerMemberId"] = "member-missing"
	payload.Datasets[string(model.Expenses)][0].Data["payerMemberId"] = "member-missing"
	payload.Datasets[string(model.Expenses)][0].Data["splitMemberIds"] = []string{"member-missing"}
	err := repo.ValidateSnapshotExternalReferences(ctx, payload)
	if !errors.Is(err, ErrSnapshotValidation) || errors.Is(err, ErrNotFound) ||
		!strings.Contains(err.Error(), "不存在于基础成员库") {
		t.Fatalf("missing base member has wrong error semantics: %v", err)
	}
	payload.Datasets[string(model.Members)][0].Data["memberId"] = memberID
	payload.Datasets[string(model.Bookings)][0].Data["bookerMemberId"] = memberID
	payload.Datasets[string(model.Expenses)][0].Data["payerMemberId"] = memberID
	payload.Datasets[string(model.Expenses)][0].Data["splitMemberIds"] = []string{memberID}

	payload.Datasets[string(model.Places)][0].CreatedByMemberID = "member-actor-missing"
	err = repo.ValidateSnapshotExternalReferences(ctx, payload)
	if !errors.Is(err, ErrSnapshotValidation) ||
		!strings.Contains(err.Error(), "createdByMemberId") {
		t.Fatalf("missing record actor was accepted: %v", err)
	}
	payload.Datasets[string(model.Places)][0].CreatedByMemberID = actorMemberID

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err = repo.ValidateSnapshotExternalReferences(cancelled, payload)
	if !errors.Is(err, context.Canceled) ||
		!errors.Is(err, ErrRepositoryUnavailable) ||
		errors.Is(err, ErrSnapshotValidation) {
		t.Fatalf("database context error was hidden as snapshot validation: %v", err)
	}
}

func TestMongoIntegrationLibraryDeletionImpactIncludesDeletedTrips(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	tag, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryTags, RecordKey: "deleted-trip-place-type",
		Data: map[string]any{
			"name": "旧地点类型", "category": model.TagCategoryPlaceType,
			"status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "deleted-impact-trip", Title: "已删除测试旅行",
		StartDate: "2026-08-01", EndDate: "2026-08-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	place, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Places, RecordKey: "deleted-trip-place",
		Data:              map[string]any{"name": "旧地点", "type": tag.ID},
		CreatedByMemberID: "member-admin", UpdatedByMemberID: "member-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DeleteTrip(ctx, trip.ID, trip.Version, "member-admin"); err != nil {
		t.Fatal(err)
	}

	impact, err := repo.GetLibraryDeletionImpact(ctx, model.LibraryTags, tag.ID)
	if err != nil {
		t.Fatalf("deleted Trip references must remain inspectable: %v", err)
	}
	if len(impact.References) != 1 {
		t.Fatalf("references=%#v", impact.References)
	}
	reference := impact.References[0]
	if reference.RecordID != place.ID ||
		reference.TripTitle != trip.Title ||
		!reference.TripDeleted ||
		!strings.Contains(strings.Join(reference.Relations, ","), "place-type") {
		t.Fatalf("deleted Trip reference is incomplete: %#v", reference)
	}
}

func TestMongoIntegrationTripMemberDeletionImpactListsEverySafeDependency(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "member-deletion-impact",
		Data: map[string]any{"displayName": "同行成员", "status": "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	expenseType, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryTags, RecordKey: "member-impact-expense-type",
		Data: map[string]any{
			"name": "餐饮", "category": model.TagCategoryExpenseType,
			"status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "member-impact-trip", Title: "成员删除影响",
		StartDate: "2026-08-01", EndDate: "2026-08-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Members, RecordKey: "selected-member",
		Data:              map[string]any{"memberId": member.ID, "role": "同行成员"},
		CreatedByMemberID: member.ID, UpdatedByMemberID: member.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Expenses, RecordKey: "shared-expense",
		Data: map[string]any{
			"item": "晚餐", "amountMinor": int64(12800), "type": expenseType.ID,
			"payerMemberId": member.ID, "splitMode": "selected",
			"splitMemberIds": []string{member.ID},
		},
		CreatedByMemberID: member.ID, UpdatedByMemberID: member.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.ResearchEvidence, RecordKey: "member-guide",
		Data:              map[string]any{"title": "景区攻略", "recordedByMemberId": member.ID},
		CreatedByMemberID: member.ID, UpdatedByMemberID: member.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.PackingItems, RecordKey: "private-item",
		Data:              map[string]any{"item": "隐私物品", "ownerMemberId": member.ID},
		CreatedByMemberID: member.ID, UpdatedByMemberID: member.ID,
	}); err != nil {
		t.Fatal(err)
	}

	impact, err := repo.GetTripMemberDeletionImpact(ctx, trip.ID, selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.References) != 1 ||
		len(impact.CascadeExpenses) != 1 ||
		!impact.HasPrivatePackingItems {
		t.Fatalf("member impact=%#v", impact)
	}
	encoded, err := json.Marshal(impact)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"expense-payer"`,
		`"expense-split"`,
		`"research-recorder"`,
		`"cascadeExpenses"`,
	} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("member impact misses %s: %s", expected, encoded)
		}
	}
	libraryImpact, err := repo.GetLibraryDeletionImpact(
		ctx,
		model.LibraryMembers,
		member.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	libraryImpactJSON, err := json.Marshal(libraryImpact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(libraryImpactJSON), "隐私物品") ||
		!strings.Contains(string(libraryImpactJSON), "个人物品（内容已隐藏）") ||
		!strings.Contains(string(libraryImpactJSON), `"packing-owner"`) {
		t.Fatalf("base member impact leaked or omitted private dependency: %s", libraryImpactJSON)
	}
	err = repo.DeleteRecord(
		ctx,
		trip.ID,
		model.Members,
		selected.ID,
		selected.Version,
	)
	if !errors.Is(err, ErrReferenced) {
		t.Fatalf("referenced member deletion error=%v", err)
	}
	for _, expected := range []string{"费用“晚餐”", "资料“景区攻略”", "个人物品"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("member deletion message misses %q: %v", expected, err)
		}
	}
}

func TestMongoIntegrationBuildSnapshotRejectsCurrentExternalReferenceDrift(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "external-drift-owner",
		Data: map[string]any{
			"displayName": "负责人", "status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryTags, RecordKey: "place-type-hotel",
		Data: map[string]any{
			"name": "住宿", "category": model.TagCategoryPlaceType,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "external-reference-drift", Title: "外部引用漂移",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Places, RecordKey: "hotel",
		Data:              map[string]any{"name": "酒店", "type": tag.ID},
		CreatedByMemberID: owner.ID, UpdatedByMemberID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	changed := map[string]any{
		"name": "住宿", "category": model.TagCategoryExpenseType,
	}
	if _, err := repo.UpdateLibraryRecord(
		ctx,
		model.LibraryTags,
		tag.ID,
		tag.Version,
		changed,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BuildSnapshot(ctx, trip.ID, "external-reference-drift"); !errors.Is(
		err,
		ErrSnapshotValidation,
	) {
		t.Fatalf("runtime snapshot accepted drifted external reference: %v", err)
	}
}

func TestMongoIntegrationNestedDaySnapshotRoundTripIsNoOp(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-owner",
		Data: map[string]any{
			"displayName": "负责人", "status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	placeType, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryTags, RecordKey: "snapshot-place-type",
		Data: map[string]any{
			"name": "景点", "category": model.TagCategoryPlaceType,
			"status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "nested-day-snapshot-noop", Title: "嵌套地点快照往返",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Members, RecordKey: "trip-owner",
		Data:              map[string]any{"memberId": owner.ID, "role": "owner"},
		CreatedByMemberID: owner.ID, UpdatedByMemberID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	place, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Places, RecordKey: "place-a",
		Data:              map[string]any{"name": "测试地点", "type": placeType.ID},
		CreatedByMemberID: owner.ID, UpdatedByMemberID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Days, RecordKey: "day-one",
		Data: map[string]any{
			"title": "第一天", "date": "2026-09-01",
			"placeEntries": []any{
				map[string]any{
					"placeId": place.ID, "arrivalTime": "09:00",
					"additionalInfo": "从北门进入",
				},
			},
		},
		CreatedByMemberID: owner.ID, UpdatedByMemberID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.BuildSnapshot(ctx, trip.ID, "nested-day-noop")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := ValidateSnapshotPayload(snapshot.Payload, trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := repo.DiffSnapshot(ctx, trip.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if diff.HasChanges || diff.Resources[string(model.Days)].Updated != 0 {
		t.Fatalf("嵌套地点快照原样预览产生了误报：%#v", diff)
	}
}

func TestMongoIntegrationSnapshotCoordinatesLibraryReferencesWithoutMutatingReadOnlyExports(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "snapshot-reference-epoch", Title: "外部引用并发闸门",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := repo.GetLibraryState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BuildSnapshot(ctx, trip.ID, "reference-epoch"); err != nil {
		t.Fatal(err)
	}
	afterSnapshot, err := repo.GetLibraryState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterSnapshot.Version != initial.Version ||
		afterSnapshot.ReferenceEpoch != initial.ReferenceEpoch+1 {
		t.Fatalf(
			"snapshot coordination changed the wrong library state: before=%#v after=%#v",
			initial,
			afterSnapshot,
		)
	}
	if _, err := repo.ExportTripBackup(ctx, trip.ID); err != nil {
		t.Fatal(err)
	}
	afterBackup, err := repo.GetLibraryState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterBackup.Version != afterSnapshot.Version ||
		afterBackup.ReferenceEpoch != afterSnapshot.ReferenceEpoch {
		t.Fatalf(
			"read-only backup changed library coordination state: before=%#v after=%#v",
			afterSnapshot,
			afterBackup,
		)
	}
	if _, err := repo.ExportLibrarySnapshot(ctx, "read-only-export"); err != nil {
		t.Fatal(err)
	}
	afterLibraryExport, err := repo.GetLibraryState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterLibraryExport.Version != afterBackup.Version ||
		afterLibraryExport.ReferenceEpoch != afterBackup.ReferenceEpoch {
		t.Fatalf(
			"read-only library export changed coordination state: before=%#v after=%#v",
			afterBackup,
			afterLibraryExport,
		)
	}
	if _, err := repo.BuildLibrarySnapshot(ctx, "transactional-build"); err != nil {
		t.Fatal(err)
	}
	afterLibrarySnapshot, err := repo.GetLibraryState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterLibrarySnapshot.Version != afterLibraryExport.Version ||
		afterLibrarySnapshot.ReferenceEpoch != afterLibraryExport.ReferenceEpoch {
		t.Fatalf(
			"library snapshot build changed coordination state: before=%#v after=%#v",
			afterLibraryExport,
			afterLibrarySnapshot,
		)
	}
	afterLibraryWrite, err := repo.BumpLibraryRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterLibraryWrite.Version != afterLibrarySnapshot.Version+1 ||
		afterLibraryWrite.ReferenceEpoch != afterLibrarySnapshot.ReferenceEpoch+1 {
		t.Fatalf(
			"library write did not participate in reference coordination: before=%#v after=%#v",
			afterLibrarySnapshot,
			afterLibraryWrite,
		)
	}
}

func TestMongoIntegrationReferenceEpochRetriesConcurrentTripAndLibraryWrites(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "concurrent-owner",
		Data: map[string]any{
			"displayName": "负责人", "status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryTags, RecordKey: "concurrent-place-type",
		Data: map[string]any{
			"name": "并发地点", "category": model.TagCategoryPlaceType,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "reference-epoch-retry", Title: "并发外部引用",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}

	firstAttemptReady := make(chan struct{})
	continueFirstAttempt := make(chan struct{})
	transactionDone := make(chan error, 1)
	attempts := 0
	go func() {
		transactionDone <- repo.WithTransaction(ctx, func(tx context.Context) error {
			attempts++
			if _, err := repo.CreateRecord(tx, model.Record{
				TripID: trip.ID, Resource: model.Places, RecordKey: "concurrent-place",
				Data:              map[string]any{"name": "并发地点", "type": tag.ID},
				CreatedByMemberID: owner.ID, UpdatedByMemberID: owner.ID,
			}); err != nil {
				return err
			}
			if _, err := repo.BumpTripRevision(tx, trip.ID); err != nil {
				return err
			}
			if attempts == 1 {
				close(firstAttemptReady)
				<-continueFirstAttempt
			}
			_, err := repo.BuildSnapshot(tx, trip.ID, "concurrent-reference")
			return err
		})
	}()

	select {
	case <-firstAttemptReady:
	case <-time.After(5 * time.Second):
		t.Fatal("Trip transaction did not reach the coordinated snapshot")
	}
	libraryErr := repo.WithTransaction(ctx, func(tx context.Context) error {
		if _, err := repo.UpdateLibraryRecord(
			tx,
			model.LibraryTags,
			tag.ID,
			tag.Version,
			map[string]any{
				"name": "并发地点", "category": model.TagCategoryExpenseType,
			},
		); err != nil {
			return err
		}
		_, err := repo.BumpLibraryRevision(tx)
		return err
	})
	close(continueFirstAttempt)
	if libraryErr != nil {
		t.Fatal(libraryErr)
	}

	select {
	case transactionErr := <-transactionDone:
		if transactionErr == nil ||
			!strings.Contains(transactionErr.Error(), "地点 type") {
			t.Fatalf(
				"retried Trip transaction did not reject the changed tag category: %v",
				transactionErr,
			)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Trip transaction did not finish after the library write")
	}
	if attempts < 2 {
		t.Fatalf("concurrent transaction was not retried: attempts=%d", attempts)
	}
	rows, err := repo.ListRecords(ctx, trip.ID, model.Places)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("aborted Trip attempt leaked records: %#v", rows)
	}
}

func TestMongoIntegrationSnapshotReplacementRejectsTripDeletionTransition(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "snapshot-delete-transition", Title: "删除状态替换防线",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := emptySnapshotPayload(t, trip)
	deletedAt := time.Now().UTC()
	payload.Trip.DeletedAt = &deletedAt
	payload.Trip.DeletedByMemberID = "member-owner"
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()

	err = repo.WithTransaction(ctx, func(tx context.Context) error {
		return repo.replaceFromSnapshot(tx, trip, payload, "member-owner")
	})
	if !errors.Is(err, ErrSnapshotValidation) ||
		!strings.Contains(err.Error(), "不能改变当前旅行的逻辑删除状态") {
		t.Fatalf("snapshot replacement accepted deletion transition: %v", err)
	}
	current, err := repo.GetTrip(ctx, trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.DeletedAt != nil || current.DeletedByMemberID != "" {
		t.Fatalf("rejected replacement changed trip deletion state: %#v", current)
	}
}

func TestMongoIntegrationSnapshotReplacementUsesAuthenticatedActorMetadata(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	creator, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-actor-creator",
		Data: map[string]any{
			"displayName": "原创建人", "status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-actor-current",
		Data: map[string]any{
			"displayName": "当前操作人", "status": "active",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "snapshot-actor-metadata", Title: "快照记录人测试",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, actor.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	existing, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Days, RecordKey: "existing-day",
		Data: map[string]any{
			"title": "旧标题", "date": "2026-09-01",
		},
		CreatedByMemberID: creator.ID, UpdatedByMemberID: creator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := emptySnapshotPayload(t, trip)
	changed := existing
	changed.Data = map[string]any{
		"title": "新标题", "date": "2026-09-01",
	}
	changed.CreatedByMemberID = "untrusted-creator"
	changed.UpdatedByMemberID = "untrusted-updater"
	added := snapshotTestRecord(
		payload,
		"new-day",
		model.Days,
		1,
		map[string]any{"title": "新增日", "date": "2026-09-02"},
	)
	added.CreatedByMemberID = "untrusted-creator"
	added.UpdatedByMemberID = "untrusted-updater"
	payload.Datasets[string(model.Days)] = []model.Record{changed, added}
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()

	if err := repo.WithTransaction(ctx, func(tx context.Context) error {
		return repo.replaceFromSnapshot(tx, trip, payload, actor.ID)
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListRecords(ctx, trip.ID, model.Days)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("days=%#v", rows)
	}
	byKey := map[string]model.Record{}
	for _, row := range rows {
		byKey[row.RecordKey] = row
	}
	if got := byKey["existing-day"]; got.CreatedByMemberID != creator.ID ||
		got.UpdatedByMemberID != actor.ID {
		t.Fatalf("existing record trusted snapshot actor metadata: %#v", got)
	}
	if got := byKey["new-day"]; got.CreatedByMemberID != actor.ID ||
		got.UpdatedByMemberID != actor.ID {
		t.Fatalf("new record trusted snapshot actor metadata: %#v", got)
	}
}

func TestMongoIntegrationSnapshotReplacementReconcilesNonOwnerMemberships(t *testing.T) {
	repo := integrationRepository(t)
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	memberIDs := make(map[string]string)
	for _, member := range []string{
		"member-owner", "member-viewer", "member-new", "member-stale",
	} {
		created, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
			Resource: model.LibraryMembers, RecordKey: member,
			Data: map[string]any{"displayName": member, "status": "active"},
		})
		if err != nil {
			t.Fatal(err)
		}
		memberIDs[member] = created.ID
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "membership-trip", Title: "权限恢复测试",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	for memberID, role := range map[string]string{
		memberIDs["member-owner"]:  "owner",
		memberIDs["member-viewer"]: "viewer",
		memberIDs["member-stale"]:  "editor",
	} {
		if err := repo.SetMembership(ctx, trip.ID, memberID, role); err != nil {
			t.Fatal(err)
		}
	}

	payload := emptySnapshotPayload(t, trip)
	payload.Datasets[string(model.Members)] = []model.Record{
		snapshotTestRecord(
			payload, "trip-member-viewer", model.Members, 0,
			map[string]any{"memberId": memberIDs["member-viewer"], "role": "owner"},
		),
		snapshotTestRecord(
			payload, "trip-member-new", model.Members, 1,
			map[string]any{"memberId": memberIDs["member-new"], "role": "viewer"},
		),
	}
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()
	preview, err := repo.DiffSnapshot(ctx, trip.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if preview.DeleteCount != 1 ||
		len(preview.MembershipAccess.Created) != 1 ||
		preview.MembershipAccess.Created[0].MemberID != memberIDs["member-new"] ||
		preview.MembershipAccess.Created[0].Role != "editor" ||
		len(preview.MembershipAccess.Deleted) != 1 ||
		preview.MembershipAccess.Deleted[0].MemberID != memberIDs["member-stale"] ||
		preview.MembershipAccess.Deleted[0].Role != "editor" {
		t.Fatalf("membership access preview is incomplete: %#v", preview)
	}
	if err := repo.WithTransaction(ctx, func(tx context.Context) error {
		return repo.replaceFromSnapshot(
			tx,
			trip,
			payload,
			memberIDs["member-owner"],
		)
	}); err != nil {
		t.Fatal(err)
	}

	for memberID, want := range map[string]string{
		memberIDs["member-owner"]:  "owner",
		memberIDs["member-viewer"]: "viewer",
		memberIDs["member-new"]:    "editor",
	} {
		if got, err := repo.RoleFor(ctx, trip.ID, memberID); err != nil || got != want {
			t.Fatalf("%s role=%q err=%v, want %q", memberID, got, err, want)
		}
	}
	if _, err := repo.RoleFor(
		ctx,
		trip.ID,
		memberIDs["member-stale"],
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale non-owner membership was preserved: %v", err)
	}

	currentPayload, _, err := repo.buildSnapshotPayload(ctx, trip.ID, "membership-only-diff")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(
		ctx,
		trip.ID,
		memberIDs["member-stale"],
		"editor",
	); err != nil {
		t.Fatal(err)
	}
	diff, err := repo.DiffSnapshot(ctx, trip.ID, currentPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.HasChanges {
		t.Fatal("membership-only drift was hidden as a no-op snapshot import")
	}
	if diff.DeleteCount != 1 ||
		len(diff.MembershipAccess.Deleted) != 1 ||
		diff.MembershipAccess.Deleted[0].MemberID != memberIDs["member-stale"] {
		t.Fatalf("membership revocation did not count as a deletion: %#v", diff)
	}
	if err := repo.WithTransaction(ctx, func(tx context.Context) error {
		_, _, err := repo.ApplySnapshot(
			tx,
			trip.ID,
			currentPayload.Revision,
			currentPayload,
			memberIDs["member-owner"],
		)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RoleFor(
		ctx,
		trip.ID,
		memberIDs["member-stale"],
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("membership-only drift was not reconciled: %v", err)
	}
}

func validSnapshotPayload(t *testing.T) model.SnapshotPayload {
	t.Helper()
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	trip := model.Trip{
		ID: "sample-trip", Title: "Sample", StartDate: "2026-09-01", EndDate: "2026-09-02",
		Version: 4, CreatedAt: now, UpdatedAt: now,
	}
	datasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		datasets[string(resource)] = []model.Record{}
	}
	record := func(id string, resource model.Resource, position int64, data map[string]any) model.Record {
		return model.Record{
			ID: id, TripID: trip.ID, Resource: resource, RecordKey: id,
			Position: position, Version: 1, Data: data,
			CreatedByMemberID: "member-owner", UpdatedByMemberID: "member-owner",
			CreatedAt: now, UpdatedAt: now,
		}
	}
	datasets["places"] = []model.Record{
		record("place-a", model.Places, 0, map[string]any{"name": "Station"}),
		record("place-b", model.Places, 1, map[string]any{"name": "Hotel"}),
	}
	datasets["days"] = []model.Record{
		record("day-a", model.Days, 0, map[string]any{
			"title": "Day 1", "date": "2026-09-01",
			"placeEntries": []any{
				map[string]any{"placeId": "place-a"},
				map[string]any{"placeId": "place-b"},
			},
		}),
	}
	datasets["segments"] = []model.Record{
		record("segment-a", model.Segments, 0, map[string]any{
			"dayId": "day-a", "fromPlaceId": "place-a", "toPlaceId": "place-b", "mode": "train",
		}),
	}
	payload := model.SnapshotPayload{
		ExportedAt: now, Reason: "test", Revision: trip.Version,
		Trip: trip, Datasets: datasets,
	}
	checksum, err := payload.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	payload.ContentChecksum = checksum
	return payload
}

func emptySnapshotPayload(t *testing.T, trip model.Trip) model.SnapshotPayload {
	t.Helper()
	datasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		datasets[string(resource)] = []model.Record{}
	}
	payload := model.SnapshotPayload{
		ExportedAt: time.Now().UTC(), Reason: "test", Revision: trip.Version,
		Trip: trip, Datasets: datasets,
	}
	payload.ContentChecksum, _ = payload.ComputedContentChecksum()
	return payload
}

func snapshotTestRecord(
	payload model.SnapshotPayload,
	id string,
	resource model.Resource,
	position int64,
	data map[string]any,
) model.Record {
	return model.Record{
		ID: id, TripID: payload.Trip.ID, Resource: resource, RecordKey: id,
		Position: position, Version: 1, Data: data,
		CreatedByMemberID: "member-owner", UpdatedByMemberID: "member-owner",
		CreatedAt: payload.ExportedAt, UpdatedAt: payload.ExportedAt,
	}
}

func snapshotPayloadJSON(t *testing.T, payload *model.SnapshotPayload) []byte {
	t.Helper()
	checksum, err := payload.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	payload.ContentChecksum = checksum
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
