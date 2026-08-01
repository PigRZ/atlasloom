// 快照服务编排镜像补偿、恢复以及受保护的快照导入。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
	"my-travel/server/internal/snapshot"
)

type SnapshotImportPreview struct {
	Valid            bool                                `json:"valid"`
	ValidationErrors []string                            `json:"validationErrors"`
	BaseRevision     int64                               `json:"baseRevision"`
	CurrentRevision  int64                               `json:"currentRevision"`
	Checksum         string                              `json:"checksum,omitempty"`
	TripChanges      map[string]repository.TripFieldDiff `json:"tripChanges"`
	DeleteCount      int                                 `json:"deleteCount"`
	Resources        map[string]repository.ResourceDiff  `json:"resources"`
	MembershipAccess repository.MembershipAccessDiff     `json:"membershipAccess"`
	HasChanges       bool                                `json:"hasChanges"`
	Message          string                              `json:"message"`
}

type pendingSnapshotStore interface {
	ListPendingSnapshots(context.Context, int64) ([]model.Snapshot, error)
	MarkSnapshotMirrored(context.Context, string, string) error
}

type pendingLibrarySnapshotStore interface {
	ListPendingLibrarySnapshots(context.Context, int64) ([]model.LibrarySnapshot, error)
	MarkLibrarySnapshotMirrored(context.Context, string, string) error
}

type snapshotReader interface {
	GetSnapshot(context.Context, string, string) (model.Snapshot, error)
}

func (s Service) WriteSnapshot(ctx context.Context, tripID, reason string) (model.Snapshot, error) {
	snapshotRecord, err := s.Repo.BuildSnapshot(ctx, tripID, reason)
	if err != nil {
		return model.Snapshot{}, err
	}
	s.mirror(ctx, tripID, &snapshotRecord)
	return snapshotRecord, nil
}

func (s Service) ReconcilePendingSnapshots(ctx context.Context, limit int64) (int, int, error) {
	mirrored, failed, err := reconcilePendingSnapshots(
		ctx,
		s.Repo,
		s.Config.SnapshotDir,
		limit,
	)
	if err != nil {
		return 0, 0, err
	}
	libraryMirrored, libraryFailed, err := reconcilePendingLibrarySnapshots(
		ctx,
		s.Repo,
		s.Config.SnapshotDir,
		limit,
	)
	return mirrored + libraryMirrored, failed + libraryFailed, err
}

func reconcilePendingSnapshots(
	ctx context.Context,
	store pendingSnapshotStore,
	root string,
	limit int64,
) (int, int, error) {
	items, err := store.ListPendingSnapshots(ctx, limit)
	if err != nil {
		return 0, 0, err
	}
	mirrored, failed := 0, 0
	for _, item := range items {
		if err := repository.VerifyStoredSnapshot(item); err != nil {
			failed++
			continue
		}
		if err := snapshot.Mirror(root, item.TripID, item.ID, item.Payload); err != nil {
			failed++
			continue
		}
		if err := store.MarkSnapshotMirrored(ctx, item.ID, "mirrored"); err != nil {
			failed++
			continue
		}
		mirrored++
	}
	return mirrored, failed, nil
}

func reconcilePendingLibrarySnapshots(
	ctx context.Context,
	store pendingLibrarySnapshotStore,
	root string,
	limit int64,
) (int, int, error) {
	items, err := store.ListPendingLibrarySnapshots(ctx, limit)
	if err != nil {
		return 0, 0, err
	}
	mirrored, failed := 0, 0
	for _, item := range items {
		if err := repository.VerifyStoredLibrarySnapshot(item); err != nil {
			failed++
			continue
		}
		if err := snapshot.Mirror(root, "library", item.ID, item.Payload); err != nil {
			failed++
			continue
		}
		if err := store.MarkLibrarySnapshotMirrored(ctx, item.ID, "mirrored"); err != nil {
			failed++
			continue
		}
		mirrored++
	}
	return mirrored, failed, nil
}

func (s Service) RestoreSnapshot(
	ctx context.Context,
	tripID, snapshotID string,
	baseRevision int64,
	allowDelete bool,
	actorMemberID string,
) (model.Snapshot, model.Snapshot, bool, error) {
	var before, after model.Snapshot
	var restored bool
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripOwner(tx, tripID, actorMemberID); err != nil {
			return err
		}
		trip, err := s.Repo.GetTrip(tx, tripID)
		if err != nil {
			return err
		}
		if trip.Version != baseRevision {
			return repository.ErrConflict
		}
		before, after, restored, err = s.Repo.RestoreSnapshot(
			tx,
			tripID,
			snapshotID,
			allowDelete,
			actorMemberID,
		)
		if err == nil && restored {
			err = s.Repo.InsertAuditLog(tx, model.AuditLog{
				TripID: tripID, MemberID: actorMemberID,
				Module: "旅行数据", Action: "恢复快照",
				Resource: "snapshots", RecordID: snapshotID, RecordLabel: snapshotID,
				Before: map[string]any{"revision": before.Revision},
				After:  map[string]any{"revision": after.Revision},
			})
		}
		return err
	})
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, err
	}
	if !restored {
		return model.Snapshot{}, model.Snapshot{}, false, nil
	}
	s.mirror(ctx, tripID, &before)
	s.mirror(ctx, tripID, &after)
	return before, after, true, nil
}

func (s Service) PreviewSnapshotImport(
	ctx context.Context,
	tripID string,
	baseRevision int64,
	raw []byte,
) (SnapshotImportPreview, error) {
	current, err := s.Repo.GetTrip(ctx, tripID)
	if err != nil {
		return SnapshotImportPreview{}, err
	}
	preview := SnapshotImportPreview{
		BaseRevision: baseRevision, CurrentRevision: current.Version,
		ValidationErrors: []string{},
		TripChanges:      map[string]repository.TripFieldDiff{},
		Resources:        map[string]repository.ResourceDiff{},
		MembershipAccess: repository.MembershipAccessDiff{
			Created: []repository.MembershipAccessChange{},
			Deleted: []repository.MembershipAccessChange{},
		},
		Message: "快照未通过校验",
	}
	payload, err := repository.ValidateSnapshotPayload(raw, tripID)
	if err != nil {
		preview.ValidationErrors = append(preview.ValidationErrors, err.Error())
		return preview, nil
	}
	if err := s.Repo.ValidateSnapshotExternalReferences(ctx, payload); err != nil {
		if !errors.Is(err, repository.ErrSnapshotValidation) {
			return SnapshotImportPreview{}, err
		}
		preview.ValidationErrors = append(preview.ValidationErrors, err.Error())
		return preview, nil
	}
	diff, err := s.Repo.DiffSnapshot(ctx, tripID, payload)
	if err != nil {
		if errors.Is(err, repository.ErrSnapshotIdentity) ||
			errors.Is(err, repository.ErrSnapshotValidation) {
			preview.ValidationErrors = append(preview.ValidationErrors, err.Error())
			return preview, nil
		}
		return SnapshotImportPreview{}, err
	}
	preview.Checksum = diff.Checksum
	preview.TripChanges = diff.TripChanges
	preview.DeleteCount = diff.DeleteCount
	preview.Resources = diff.Resources
	preview.MembershipAccess = diff.MembershipAccess
	preview.HasChanges = diff.HasChanges
	preview.Message = diff.Message
	if baseRevision != payload.Revision {
		preview.ValidationErrors = append(preview.ValidationErrors, "baseRevision must equal snapshot revision")
	}
	if current.Version != baseRevision {
		preview.ValidationErrors = append(preview.ValidationErrors, "remote revision changed after the snapshot was pulled")
	}
	preview.Valid = len(preview.ValidationErrors) == 0
	return preview, nil
}

func (s Service) ApplySnapshotImport(
	ctx context.Context,
	tripID string,
	baseRevision int64,
	raw []byte,
	confirmChecksum string,
	allowDelete bool,
	actorMemberID string,
	idempotencyKey, requestHash string,
) (model.Snapshot, model.Snapshot, bool, bool, error) {
	payload, err := repository.ValidateSnapshotPayload(raw, tripID)
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, false, err
	}
	if payload.Revision != baseRevision {
		return model.Snapshot{}, model.Snapshot{}, false, false, repository.ErrConflict
	}
	if confirmChecksum == "" || confirmChecksum != payload.ContentChecksum {
		return model.Snapshot{}, model.Snapshot{}, false, false, fmt.Errorf("confirmChecksum must match contentChecksum")
	}
	type result struct {
		Before  model.Snapshot `json:"before"`
		After   model.Snapshot `json:"after"`
		Applied bool           `json:"applied"`
	}
	scope := "snapshot-import:" + tripID
	var output result
	var replayed bool
	err = s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireTripOwner(tx, tripID, actorMemberID); err != nil {
			return err
		}
		if stored, err := s.Repo.GetIdempotencyRequest(tx, scope, idempotencyKey, requestHash); err == nil {
			replayed = true
			return json.Unmarshal(stored.Response, &output)
		} else if !errors.Is(err, repository.ErrNotFound) {
			return err
		}
		if err := s.Repo.ValidateSnapshotExternalReferences(tx, payload); err != nil {
			return err
		}
		diff, err := s.Repo.DiffSnapshot(tx, tripID, payload)
		if err != nil {
			return err
		}
		if diff.CurrentRevision != baseRevision {
			return repository.ErrConflict
		}
		if diff.DeleteCount > 0 && !allowDelete {
			return fmt.Errorf(
				"快照将删除 %d 项资料或成员访问权；请先检查预览并明确允许删除",
				diff.DeleteCount,
			)
		}
		if !diff.HasChanges {
			output = result{Applied: false}
			encoded, err := json.Marshal(output)
			if err != nil {
				return err
			}
			return s.Repo.StoreIdempotencyRequest(tx, scope, idempotencyKey, requestHash, encoded)
		}
		before, after, err := s.Repo.ApplySnapshot(
			tx,
			tripID,
			baseRevision,
			payload,
			actorMemberID,
		)
		if err != nil {
			return err
		}
		output = result{Before: before, After: after, Applied: true}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			TripID: tripID, MemberID: actorMemberID,
			Module: "旅行数据", Action: "导入快照",
			Resource: "snapshot-imports", RecordLabel: "本地快照",
			Before: map[string]any{"revision": before.Revision},
			After: map[string]any{
				"revision": after.Revision, "deleteCount": diff.DeleteCount,
			},
		}); err != nil {
			return err
		}
		encoded, err := json.Marshal(output)
		if err != nil {
			return err
		}
		return s.Repo.StoreIdempotencyRequest(tx, scope, idempotencyKey, requestHash, encoded)
	})
	if err != nil {
		return model.Snapshot{}, model.Snapshot{}, false, false, err
	}
	if replayed {
		if output.Applied {
			if err := refreshSnapshots(ctx, s.Repo, &output.Before, &output.After); err != nil {
				return model.Snapshot{}, model.Snapshot{}, false, false, err
			}
		}
	} else if output.Applied {
		s.mirror(ctx, tripID, &output.Before)
		s.mirror(ctx, tripID, &output.After)
	}
	return output.Before, output.After, output.Applied, replayed, nil
}

func refreshSnapshots(ctx context.Context, reader snapshotReader, items ...*model.Snapshot) error {
	for _, item := range items {
		current, err := reader.GetSnapshot(ctx, item.TripID, item.ID)
		if err != nil {
			return err
		}
		*item = current
	}
	return nil
}

func (s Service) mirror(ctx context.Context, tripID string, item *model.Snapshot) {
	if item.ID == "" || len(item.Payload) == 0 {
		return
	}
	if err := snapshot.Mirror(s.Config.SnapshotDir, tripID, item.ID, item.Payload); err != nil {
		_ = s.Repo.MarkSnapshotMirrored(context.Background(), item.ID, "pending")
		return
	}
	if err := s.Repo.MarkSnapshotMirrored(ctx, item.ID, "mirrored"); err == nil {
		item.MirrorState = "mirrored"
	}
}
