// 快照成员访问权集成测试固定预览、删除确认与事务应用使用同一份权限差异。
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
	"my-travel/server/internal/service"
)

func TestMongoIntegrationSnapshotImportRejectsTripDeletionTransition(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	repo, err := repository.Connect(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-delete-owner",
		Data: map[string]any{"displayName": "负责人", "status": "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "snapshot-import-delete-transition", Title: "删除状态导入防线",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.BuildSnapshot(ctx, trip.ID, "active-trip")
	if err != nil {
		t.Fatal(err)
	}
	var payload model.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC()
	payload.Trip.DeletedAt = &deletedAt
	payload.Trip.DeletedByMemberID = owner.ID
	payload.ContentChecksum, err = payload.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	snapshotService := service.Service{Repo: repo, Config: config}
	preview, err := snapshotService.PreviewSnapshotImport(
		ctx,
		trip.ID,
		trip.Version,
		raw,
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Valid ||
		len(preview.ValidationErrors) != 1 ||
		!strings.Contains(preview.ValidationErrors[0], "不能改变当前旅行的逻辑删除状态") {
		t.Fatalf("deletion transition was not exposed as an invalid preview: %#v", preview)
	}

	_, _, applied, _, err := snapshotService.ApplySnapshotImport(
		ctx,
		trip.ID,
		trip.Version,
		raw,
		payload.ContentChecksum,
		true,
		owner.ID,
		"trip-delete-transition",
		strings.Repeat("d", 64),
	)
	if !errors.Is(err, repository.ErrSnapshotValidation) || applied {
		t.Fatalf("deletion transition was applied: applied=%v err=%v", applied, err)
	}
	current, err := repo.GetTrip(ctx, trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.DeletedAt != nil || current.DeletedByMemberID != "" {
		t.Fatalf("rejected import changed trip deletion state: %#v", current)
	}
}

func TestMongoIntegrationSnapshotApplyValidatesNoOpExternalReferences(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	repo, err := repository.Connect(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-noop-owner",
		Data: map[string]any{"displayName": "负责人", "status": "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "snapshot-noop-external-reference", Title: "无变化外部引用校验",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRecord(ctx, model.Record{
		TripID: trip.ID, Resource: model.Days, RecordKey: "day-one",
		Data: map[string]any{"title": "第一天", "date": "2026-09-01"},
		CreatedByMemberID: owner.ID, UpdatedByMemberID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.BuildSnapshot(ctx, trip.ID, "valid-noop-base")
	if err != nil {
		t.Fatal(err)
	}
	var payload model.SnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Datasets[string(model.Days)][0].CreatedByMemberID = "missing-member"
	payload.ContentChecksum, err = payload.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	snapshotService := service.Service{Repo: repo, Config: config}
	preview, err := snapshotService.PreviewSnapshotImport(
		ctx, trip.ID, trip.Version, raw,
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Valid || len(preview.ValidationErrors) == 0 {
		t.Fatalf("preview accepted missing record actor: %#v", preview)
	}
	_, _, applied, _, err := snapshotService.ApplySnapshotImport(
		ctx,
		trip.ID,
		trip.Version,
		raw,
		payload.ContentChecksum,
		false,
		owner.ID,
		"noop-invalid-external-reference",
		strings.Repeat("e", 64),
	)
	if !errors.Is(err, repository.ErrSnapshotValidation) || applied {
		t.Fatalf("no-op apply bypassed external validation: applied=%v err=%v", applied, err)
	}
}

func TestMongoIntegrationSnapshotMembershipRevocationRequiresAllowDelete(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	repo, err := repository.Connect(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	ctx := context.Background()
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-owner",
		Data: map[string]any{"displayName": "负责人", "status": "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
		Resource: model.LibraryMembers, RecordKey: "snapshot-stale-editor",
		Data: map[string]any{"displayName": "待撤权成员", "status": "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trip, err := repo.CreateTrip(ctx, model.Trip{
		ID: "snapshot-membership-delete-gate", Title: "成员撤权确认",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMembership(ctx, trip.ID, stale.ID, "editor"); err != nil {
		t.Fatal(err)
	}
	target, err := repo.BuildSnapshot(ctx, trip.ID, "membership-delete-target")
	if err != nil {
		t.Fatal(err)
	}

	snapshotService := service.Service{Repo: repo, Config: config}
	preview, err := snapshotService.PreviewSnapshotImport(
		ctx,
		trip.ID,
		trip.Version,
		target.Payload,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Valid || preview.DeleteCount != 1 ||
		len(preview.MembershipAccess.Deleted) != 1 ||
		preview.MembershipAccess.Deleted[0].MemberID != stale.ID ||
		preview.MembershipAccess.Deleted[0].Role != "editor" {
		t.Fatalf("membership revocation preview is incomplete: %#v", preview)
	}

	_, _, restored, err := snapshotService.RestoreSnapshot(
		ctx,
		trip.ID,
		target.ID,
		trip.Version,
		false,
		owner.ID,
	)
	if err == nil || !strings.Contains(err.Error(), "明确允许删除") || restored {
		t.Fatalf("stored restore bypassed deletion confirmation: restored=%v err=%v", restored, err)
	}
	if role, err := repo.RoleFor(ctx, trip.ID, stale.ID); err != nil || role != "editor" {
		t.Fatalf("rejected restore changed stale membership: role=%q err=%v", role, err)
	}

	_, _, applied, _, err := snapshotService.ApplySnapshotImport(
		ctx,
		trip.ID,
		trip.Version,
		target.Payload,
		target.ContentChecksum,
		false,
		owner.ID,
		"membership-delete-denied",
		strings.Repeat("a", 64),
	)
	if err == nil || !strings.Contains(err.Error(), "删除 1 项") || applied {
		t.Fatalf("membership revocation bypassed allowDelete: applied=%v err=%v", applied, err)
	}
	if role, err := repo.RoleFor(ctx, trip.ID, stale.ID); err != nil || role != "editor" {
		t.Fatalf("failed apply changed stale membership: role=%q err=%v", role, err)
	}

	_, _, applied, _, err = snapshotService.ApplySnapshotImport(
		ctx,
		trip.ID,
		trip.Version,
		target.Payload,
		target.ContentChecksum,
		true,
		owner.ID,
		"membership-delete-confirmed",
		strings.Repeat("b", 64),
	)
	if err != nil || !applied {
		t.Fatalf("confirmed membership revocation failed: applied=%v err=%v", applied, err)
	}
	if _, err := repo.RoleFor(ctx, trip.ID, stale.ID); !errors.Is(
		err,
		repository.ErrNotFound,
	) {
		t.Fatalf("confirmed membership revocation was not applied: %v", err)
	}

	if err := repo.SetMembership(ctx, trip.ID, stale.ID, "editor"); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetTrip(ctx, trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, restored, err = snapshotService.RestoreSnapshot(
		ctx,
		trip.ID,
		target.ID,
		current.Version,
		true,
		owner.ID,
	)
	if err != nil || !restored {
		t.Fatalf("confirmed stored restore failed: restored=%v err=%v", restored, err)
	}
	if _, err := repo.RoleFor(ctx, trip.ID, stale.ID); !errors.Is(
		err,
		repository.ErrNotFound,
	) {
		t.Fatalf("confirmed stored restore did not revoke membership: %v", err)
	}
}
