package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

func TestValidateSnapshotFileUsesCurrentRuntimeContract(t *testing.T) {
	payload := currentSnapshotFixture(t)
	path := writeSnapshotFixture(t, "snapshot.json", payload)

	summary, err := validateSnapshotFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Valid || summary.TripID != payload.Trip.ID ||
		summary.ContentChecksum != payload.ContentChecksum {
		t.Fatalf("unexpected validation summary: %#v", summary)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"unexpected":true}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSnapshotFile(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown envelope field was not rejected: %v", err)
	}
}

func TestValidateTripBackupFileRechecksPortableBackupChecksum(t *testing.T) {
	payload := currentSnapshotFixture(t)
	backup := model.TripBackup{
		Kind:       model.TripBackupKind,
		ExportedAt: payload.ExportedAt,
		Snapshot:   payload,
		Dependencies: model.TripBackupDependencies{
			Members: []model.TripBackupMemberDependency{{
				SourceID: "member-a", DisplayName: "成员 A",
			}},
			Tags: []model.TripBackupTagDependency{},
		},
	}
	var err error
	backup.ContentChecksum, err = backup.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "admin-backup")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := validateTripBackupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Valid || summary.TripID != payload.Trip.ID ||
		summary.Revision != payload.Revision ||
		summary.ContentChecksum != backup.ContentChecksum {
		t.Fatalf("unexpected trip backup validation summary: %#v", summary)
	}

	backup.Dependencies.Members[0].DisplayName = "篡改后的成员"
	raw, err = json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTripBackupFile(path); err == nil ||
		!strings.Contains(err.Error(), "校验和不匹配") {
		t.Fatalf("tampered portable backup checksum was not rejected: %v", err)
	}

	backup.Dependencies.Members = nil
	backup.ContentChecksum, err = backup.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTripBackupFile(path); err == nil ||
		!strings.Contains(err.Error(), "成员依赖") {
		t.Fatalf("portable backup with incomplete dependencies was not rejected: %v", err)
	}
}

func TestPrepareSnapshotSortsRechecksumsAndRefusesOverwrite(t *testing.T) {
	payload := currentSnapshotFixture(t)
	payload.Datasets["days"][0], payload.Datasets["days"][1] =
		payload.Datasets["days"][1], payload.Datasets["days"][0]
	payload.Datasets["places"][0], payload.Datasets["places"][1] =
		payload.Datasets["places"][1], payload.Datasets["places"][0]
	payload.ContentChecksum = "stale-after-local-edit"
	input := writeSnapshotFixture(t, "working.json", payload)
	output := filepath.Join(t.TempDir(), "prepared.json")

	summary, err := prepareSnapshotFile(input, output, 8, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Revision != 8 || summary.ContentChecksum == payload.ContentChecksum {
		t.Fatalf("snapshot was not prepared: %#v", summary)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := repository.ValidateSnapshotPayload(raw, payload.Trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Trip.Version != 8 ||
		validated.Datasets["days"][0].RecordKey != "day-a" ||
		validated.Datasets["places"][0].RecordKey != "place-a" {
		t.Fatalf("prepared snapshot is not canonical: %#v", validated)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("prepared snapshot mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := prepareSnapshotFile(input, output, 8, true); err == nil ||
		!strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("existing output was not protected: %v", err)
	}
}

func TestDiffSnapshotFilesComparesMeaningByRecordKey(t *testing.T) {
	base := currentSnapshotFixture(t)
	base.Datasets["packing-items"] = []model.Record{
		fixtureRecord(base.Trip, "pack-a", model.PackingItems, "pack-a", 0, map[string]any{
			"item": "A", "ownerMemberId": "member-a",
		}),
		fixtureRecord(base.Trip, "pack-b", model.PackingItems, "pack-b", 1, map[string]any{
			"item": "B", "ownerMemberId": "member-a",
		}),
	}
	refreshChecksum(t, &base)

	incoming := cloneSnapshotFixture(t, base)
	incoming.Trip.Title = "Changed Trip title"
	incoming.Datasets["packing-items"] = []model.Record{
		fixtureRecord(incoming.Trip, "pack-a", model.PackingItems, "pack-a", 0, map[string]any{
			"item": "A changed", "ownerMemberId": "member-a",
		}),
		fixtureRecord(incoming.Trip, "pack-c", model.PackingItems, "pack-c", 1, map[string]any{
			"item": "C", "ownerMemberId": "member-a",
		}),
	}
	refreshChecksum(t, &incoming)

	diff, err := diffSnapshotFiles(
		writeSnapshotFixture(t, "base.json", base),
		writeSnapshotFixture(t, "incoming.json", incoming),
	)
	if err != nil {
		t.Fatal(err)
	}
	got := diff.Resources["packing-items"]
	if got.Current != 2 || got.Incoming != 2 || got.Created != 1 ||
		got.Updated != 1 || got.Deleted != 1 || diff.DeleteCount != 1 {
		t.Fatalf("unexpected diff: %#v", diff)
	}
	if title, ok := diff.TripChanges["title"]; !ok ||
		title.Current != "Sample" || title.Incoming != "Changed Trip title" {
		t.Fatalf("Trip metadata diff missing: %#v", diff.TripChanges)
	}
	if diff.MembershipAccess.Created == nil ||
		diff.MembershipAccess.Deleted == nil ||
		len(diff.MembershipAccess.Created) != 0 ||
		len(diff.MembershipAccess.Deleted) != 0 {
		t.Fatalf("offline diff must expose empty membership access changes: %#v", diff)
	}
}

func TestDiffSnapshotFilesRejectsRecordIdentityChange(t *testing.T) {
	base := currentSnapshotFixture(t)
	incoming := cloneSnapshotFixture(t, base)
	incoming.Datasets["places"][0].ID = "different-id"
	incoming.Datasets["days"][0].Data["placeEntries"] = []any{
		map[string]any{"placeId": "different-id"},
		map[string]any{"placeId": "place-b"},
	}
	incoming.Datasets["segments"][0].Data["fromPlaceId"] = "different-id"
	refreshChecksum(t, &incoming)

	_, err := diffSnapshotFiles(
		writeSnapshotFixture(t, "base.json", base),
		writeSnapshotFixture(t, "incoming.json", incoming),
	)
	if err == nil || !strings.Contains(err.Error(), repository.ErrSnapshotIdentity.Error()) {
		t.Fatalf("record identity change was not rejected: %v", err)
	}
}

func TestDiffSnapshotFilesRejectsDeleteAndReuseOfExistingID(t *testing.T) {
	base := currentSnapshotFixture(t)
	incoming := cloneSnapshotFixture(t, base)
	incoming.Datasets["places"][0].RecordKey = "replacement-place"
	refreshChecksum(t, &incoming)

	_, err := diffSnapshotFiles(
		writeSnapshotFixture(t, "base.json", base),
		writeSnapshotFixture(t, "incoming.json", incoming),
	)
	if err == nil || !strings.Contains(err.Error(), repository.ErrSnapshotIdentity.Error()) {
		t.Fatalf("delete-and-reuse identity change was not rejected: %v", err)
	}
}

func TestDiffSnapshotFilesReportsAlreadySynchronized(t *testing.T) {
	base := currentSnapshotFixture(t)
	incoming := cloneSnapshotFixture(t, base)

	diff, err := diffSnapshotFiles(
		writeSnapshotFixture(t, "base.json", base),
		writeSnapshotFixture(t, "incoming.json", incoming),
	)
	if err != nil {
		t.Fatal(err)
	}
	if diff.HasChanges || diff.Message != repository.SnapshotNoChangesMessage {
		t.Fatalf("identical snapshots were not reported as synchronized: %#v", diff)
	}
}

func currentSnapshotFixture(t *testing.T) model.SnapshotPayload {
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
	datasets["days"] = []model.Record{
		fixtureRecord(trip, "day-a", model.Days, "day-a", 0, map[string]any{
			"title": "Day 1", "date": "2026-09-01",
			"placeEntries": []any{
				map[string]any{"placeId": "place-a"},
				map[string]any{"placeId": "place-b"},
			},
		}),
		fixtureRecord(trip, "day-b", model.Days, "day-b", 1, map[string]any{
			"title": "Day 2", "date": "2026-09-02",
		}),
	}
	datasets["places"] = []model.Record{
		fixtureRecord(trip, "place-a", model.Places, "place-a", 0, map[string]any{"name": "Station"}),
		fixtureRecord(trip, "place-b", model.Places, "place-b", 1, map[string]any{"name": "Hotel"}),
	}
	datasets["segments"] = []model.Record{
		fixtureRecord(trip, "segment-a", model.Segments, "segment-a", 0, map[string]any{
			"dayId": "day-a", "fromPlaceId": "place-a", "toPlaceId": "place-b", "mode": "train",
		}),
	}
	datasets["members"] = []model.Record{
		fixtureRecord(trip, "trip-member-a", model.Members, "trip-member-a", 0, map[string]any{
			"memberId": "member-a", "role": "同行成员",
		}),
	}
	payload := model.SnapshotPayload{
		ExportedAt: now,
		Reason:     "test",
		Revision:   trip.Version,
		Trip:       trip,
		Datasets:   datasets,
	}
	refreshChecksum(t, &payload)
	return payload
}

func fixtureRecord(
	trip model.Trip,
	id string,
	resource model.Resource,
	recordKey string,
	position int64,
	data map[string]any,
) model.Record {
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	return model.Record{
		ID: id, TripID: trip.ID, Resource: resource, RecordKey: recordKey,
		Position: position, Version: 1, Data: data,
		CreatedByMemberID: "member-a", UpdatedByMemberID: "member-a",
		CreatedAt: now, UpdatedAt: now,
	}
}

func refreshChecksum(t *testing.T, payload *model.SnapshotPayload) {
	t.Helper()
	checksum, err := payload.ComputedContentChecksum()
	if err != nil {
		t.Fatal(err)
	}
	payload.ContentChecksum = checksum
}

func writeSnapshotFixture(t *testing.T, name string, payload model.SnapshotPayload) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cloneSnapshotFixture(t *testing.T, payload model.SnapshotPayload) model.SnapshotPayload {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var cloned model.SnapshotPayload
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
