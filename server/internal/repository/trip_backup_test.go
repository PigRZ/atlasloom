package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"my-travel/server/internal/model"
)

func TestValidateTripBackupRequiresExactDependenciesAndChecksum(t *testing.T) {
	backup := validTripBackup(t)
	raw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateTripBackup(raw); err != nil {
		t.Fatalf("valid backup was rejected: %v", err)
	}

	backup.Dependencies.Members = nil
	backup.ContentChecksum, _ = backup.ComputedContentChecksum()
	raw, _ = json.Marshal(backup)
	if _, err := ValidateTripBackup(raw); err == nil ||
		!strings.Contains(err.Error(), "缺少成员依赖") {
		t.Fatalf("missing member dependency was accepted: %v", err)
	}

	backup = validTripBackup(t)
	backup.Snapshot.Trip.Title = "被篡改"
	raw, _ = json.Marshal(backup)
	if _, err := ValidateTripBackup(raw); err == nil ||
		(!strings.Contains(err.Error(), "校验和") &&
			!strings.Contains(err.Error(), "contentChecksum")) {
		t.Fatalf("tampered backup was accepted: %v", err)
	}
}

func TestPrepareTripBackupRecordsRemapsIdentityAndReferences(t *testing.T) {
	backup := validTripBackup(t)
	target := model.Trip{ID: "imported-trip", Title: "导入", Version: 1}
	rows, memberIDs, err := prepareTripBackupRecords(
		backup.Snapshot,
		target,
		map[string]string{"member-source": "member-local"},
		map[string]string{},
		"member-local",
	)
	if err != nil {
		t.Fatal(err)
	}
	byResource := map[model.Resource][]model.Record{}
	for _, row := range rows {
		if row.TripID != target.ID || row.ID == "" || row.ID == row.RecordKey {
			t.Fatalf("record identity was not reset: %#v", row)
		}
		if row.Version != 1 || row.CreatedByMemberID != "member-local" {
			t.Fatalf("record metadata was not reset: %#v", row)
		}
		byResource[row.Resource] = append(byResource[row.Resource], row)
	}
	member := byResource[model.Members][0]
	if member.Data["memberId"] != "member-local" ||
		member.Data["role"] != "owner" {
		t.Fatalf("member mapping or owner role missing: %#v", member.Data)
	}
	day := byResource[model.Days][0]
	placeID := dayPlaceEntries(day.Data["placeEntries"])[0]["placeId"]
	if placeID == "place-a" || placeID == "" {
		t.Fatalf("day place reference was not remapped: %#v", day.Data)
	}
	if _, ok := memberIDs["member-local"]; !ok {
		t.Fatalf("mapped member was not returned: %#v", memberIDs)
	}
}

func TestTripBackupReferenceIDsIncludePlatformAndResearchRecorder(t *testing.T) {
	payload := validSnapshotPayload(t)
	payload.Datasets[string(model.Bookings)] = []model.Record{{
		Data: map[string]any{
			"item": "车票", "platformId": "platform-12306",
			"type": "expense-transport", "bookerMemberId": "member-booker",
		},
	}}
	payload.Datasets[string(model.ResearchEvidence)] = []model.Record{{
		Data: map[string]any{
			"title":              "攻略",
			"platformId":         "platform-xiaohongshu",
			"recordedByMemberId": "member-recorder",
		},
	}}

	members, tags := tripBackupReferenceIDs(payload)
	if _, ok := members["member-recorder"]; !ok {
		t.Fatalf("research recorder missing from dependencies: %#v", members)
	}
	if _, ok := members["member-booker"]; !ok {
		t.Fatalf("booking booker missing from dependencies: %#v", members)
	}
	for _, id := range []string{
		"platform-12306", "platform-xiaohongshu", "expense-transport",
	} {
		if _, ok := tags[id]; !ok {
			t.Fatalf("platform %s missing from dependencies: %#v", id, tags)
		}
	}
}

func validTripBackup(t *testing.T) model.TripBackup {
	t.Helper()
	snapshot := validSnapshotPayload(t)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	for resource, rows := range snapshot.Datasets {
		for index := range rows {
			rows[index].CreatedByMemberID = "member-source"
			rows[index].UpdatedByMemberID = "member-source"
		}
		snapshot.Datasets[resource] = rows
	}
	snapshot.Datasets[string(model.Members)] = []model.Record{{
		ID: "trip-member-a", TripID: snapshot.Trip.ID, Resource: model.Members,
		RecordKey: "trip-member-a", Position: 0, Version: 1,
		Data: map[string]any{
			"memberId": "member-source",
			"role":     "owner",
		},
		CreatedByMemberID: "member-source", UpdatedByMemberID: "member-source",
		CreatedAt: now, UpdatedAt: now,
	}}
	snapshot.ContentChecksum, _ = snapshot.ComputedContentChecksum()
	backup := model.TripBackup{
		Kind:       model.TripBackupKind,
		ExportedAt: now,
		Snapshot:   snapshot,
		Dependencies: model.TripBackupDependencies{
			Members: []model.TripBackupMemberDependency{{
				SourceID: "member-source", DisplayName: "来源成员",
			}},
			Tags: []model.TripBackupTagDependency{},
		},
	}
	backup.ContentChecksum, _ = backup.ComputedContentChecksum()
	return backup
}
