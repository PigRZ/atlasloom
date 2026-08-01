package service

import (
	"context"
	"errors"
	"testing"

	"my-travel/server/internal/model"
)

type snapshotReaderStub struct {
	items map[string]model.Snapshot
	err   error
}

func (stub snapshotReaderStub) GetSnapshot(_ context.Context, tripID, snapshotID string) (model.Snapshot, error) {
	if stub.err != nil {
		return model.Snapshot{}, stub.err
	}
	item, ok := stub.items[tripID+"/"+snapshotID]
	if !ok {
		return model.Snapshot{}, errors.New("snapshot not found")
	}
	return item, nil
}

func TestRefreshSnapshotsReturnsCurrentMirrorStateForIdempotentReplay(t *testing.T) {
	before := model.Snapshot{ID: "before", TripID: "trip-1", MirrorState: "pending"}
	after := model.Snapshot{ID: "after", TripID: "trip-1", MirrorState: "pending"}
	reader := snapshotReaderStub{items: map[string]model.Snapshot{
		"trip-1/before": {ID: "before", TripID: "trip-1", MirrorState: "mirrored"},
		"trip-1/after":  {ID: "after", TripID: "trip-1", MirrorState: "mirrored"},
	}}

	if err := refreshSnapshots(context.Background(), reader, &before, &after); err != nil {
		t.Fatal(err)
	}
	if before.MirrorState != "mirrored" || after.MirrorState != "mirrored" {
		t.Fatalf("stale snapshot states remained: before=%q after=%q", before.MirrorState, after.MirrorState)
	}
}

func TestRefreshSnapshotsPropagatesReadFailure(t *testing.T) {
	item := model.Snapshot{ID: "snapshot-1", TripID: "trip-1", MirrorState: "pending"}
	if err := refreshSnapshots(
		context.Background(),
		snapshotReaderStub{err: errors.New("read failed")},
		&item,
	); err == nil {
		t.Fatal("snapshot read failure was ignored")
	}
}
