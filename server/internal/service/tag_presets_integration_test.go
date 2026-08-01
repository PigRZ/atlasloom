package service

import (
	"strings"
	"testing"

	"my-travel/server/internal/model"
)

func TestMongoIntegrationTravelTypePresetsAreAtomicAndIdempotent(t *testing.T) {
	ctx, travelService, repo := authorizationIntegrationService(t)
	admin := createAuthorizationMember(t, ctx, repo, "preset-admin", true)
	hash := strings.Repeat("b", 64)

	created, snapshot, replayed, err := travelService.AddTravelTypeTagPresets(
		ctx,
		admin.ID,
		"preset-seed-first",
		hash,
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || snapshot == nil || len(created) != len(model.TravelTypeTagPresets()) {
		t.Fatalf(
			"unexpected initial preset result: created=%d snapshot=%v replayed=%v",
			len(created),
			snapshot != nil,
			replayed,
		)
	}

	replayedRows, replayedSnapshot, replayed, err := travelService.AddTravelTypeTagPresets(
		ctx,
		admin.ID,
		"preset-seed-first",
		hash,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || replayedSnapshot == nil || len(replayedRows) != len(created) {
		t.Fatalf(
			"preset replay did not reuse the original result: created=%d snapshot=%v replayed=%v",
			len(replayedRows),
			replayedSnapshot != nil,
			replayed,
		)
	}

	missing, noSnapshot, replayed, err := travelService.AddTravelTypeTagPresets(
		ctx,
		admin.ID,
		"preset-seed-noop",
		strings.Repeat("c", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || len(missing) != 0 || noSnapshot != nil {
		t.Fatalf(
			"complete preset catalog should be a no-op: created=%d snapshot=%v replayed=%v",
			len(missing),
			noSnapshot != nil,
			replayed,
		)
	}
}
