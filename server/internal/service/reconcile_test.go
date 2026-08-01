// 这些测试固定 pending 镜像只在校验通过并原子写入后才标记完成。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"my-travel/server/internal/model"
)

type fakePendingStore struct {
	items  []model.Snapshot
	marked []string
}

type fakePendingLibraryStore struct {
	items  []model.LibrarySnapshot
	marked []string
}

func (f *fakePendingStore) ListPendingSnapshots(context.Context, int64) ([]model.Snapshot, error) {
	return f.items, nil
}

func (f *fakePendingStore) MarkSnapshotMirrored(_ context.Context, id, state string) error {
	if state == "mirrored" {
		f.marked = append(f.marked, id)
	}
	return nil
}

func (f *fakePendingLibraryStore) ListPendingLibrarySnapshots(
	context.Context,
	int64,
) ([]model.LibrarySnapshot, error) {
	return f.items, nil
}

func (f *fakePendingLibraryStore) MarkLibrarySnapshotMirrored(
	_ context.Context,
	id, state string,
) error {
	if state == "mirrored" {
		f.marked = append(f.marked, id)
	}
	return nil
}

func TestReconcilePendingSnapshotsMirrorsOnlyVerifiedPayload(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	trip := model.Trip{
		ID: "trip-a", Title: "Trip", StartDate: "2026-09-01", EndDate: "2026-09-01",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	datasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		datasets[string(resource)] = []model.Record{}
	}
	snapshotPayload := model.SnapshotPayload{
		ExportedAt: now,
		Reason:     "test",
		Revision:   trip.Version,
		Trip:       trip,
		Datasets:   datasets,
	}
	var err error
	snapshotPayload.ContentChecksum, err = snapshotPayload.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(snapshotPayload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	good := model.Snapshot{
		ID: "snapshot-good", TripID: "trip-a",
		Revision:        1,
		ContentChecksum: snapshotPayload.ContentChecksum,
		Payload:         payload,
		SHA256:          hex.EncodeToString(sum[:]),
		CreatedAt:       time.Now().UTC(),
	}
	bad := good
	bad.ID = "snapshot-bad"
	bad.SHA256 = "bad"
	store := &fakePendingStore{items: []model.Snapshot{good, bad}}

	mirrored, failed, err := reconcilePendingSnapshots(context.Background(), store, root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if mirrored != 1 || failed != 1 {
		t.Fatalf("mirrored=%d failed=%d", mirrored, failed)
	}
	if len(store.marked) != 1 || store.marked[0] != good.ID {
		t.Fatalf("marked=%v", store.marked)
	}
	if _, err := os.Stat(filepath.Join(root, good.TripID, good.ID+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, bad.TripID, bad.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("damaged payload was mirrored")
	}
}

func TestReconcilePendingLibrarySnapshots(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)
	datasets := make(map[string][]model.LibraryRecord, len(model.LibraryResources()))
	for _, resource := range model.LibraryResources() {
		datasets[string(resource)] = []model.LibraryRecord{}
	}
	content := model.LibrarySnapshotPayload{
		ExportedAt: now,
		Reason:     "test",
		Revision:   1,
		Datasets:   datasets,
	}
	var err error
	content.ContentChecksum, err = content.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	good := model.LibrarySnapshot{
		ID:       "library-good",
		Revision: 1, ContentChecksum: content.ContentChecksum,
		Payload: payload, SHA256: hex.EncodeToString(sum[:]), CreatedAt: now,
	}
	bad := good
	bad.ID = "library-bad"
	bad.SHA256 = "bad"
	store := &fakePendingLibraryStore{items: []model.LibrarySnapshot{good, bad}}

	mirrored, failed, err := reconcilePendingLibrarySnapshots(
		context.Background(),
		store,
		root,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if mirrored != 1 || failed != 1 {
		t.Fatalf("mirrored=%d failed=%d", mirrored, failed)
	}
	if len(store.marked) != 1 || store.marked[0] != good.ID {
		t.Fatalf("marked=%v", store.marked)
	}
	if _, err := os.Stat(filepath.Join(root, "library", good.ID+".json")); err != nil {
		t.Fatal(err)
	}
}
