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

func TestBuildTripBackupFileProducesCurrentImportContract(t *testing.T) {
	input := writePlanFixture(t, validTripPlanFixture())
	output := filepath.Join(t.TempDir(), "trip-backup.json")

	summary, err := buildTripBackupFile(input, output)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Valid || summary.TripID != "sample-city-weekend" ||
		summary.RecordCounts[string(model.Days)] != 2 ||
		summary.RecordCounts[string(model.Places)] != 2 ||
		summary.RecordCounts[string(model.ResearchEvidence)] != 1 {
		t.Fatalf("unexpected build summary: %#v", summary)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := repository.ValidateTripBackup(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(backup.Dependencies.Members) != 1 ||
		len(backup.Dependencies.Tags) != 3 {
		t.Fatalf("unexpected dependencies: %#v", backup.Dependencies)
	}
	if backup.Snapshot.Trip.Summary != "适合验证当前计划编译契约的示例旅行。" {
		t.Fatalf("trip summary was not compiled: %#v", backup.Snapshot.Trip)
	}
	for _, resource := range model.Resources() {
		for _, row := range backup.Snapshot.Datasets[string(resource)] {
			if row.CreatedByMemberID != "member:traveler" ||
				row.UpdatedByMemberID != "member:traveler" {
				t.Fatalf(
					"%s record %s did not keep the plan owner as actor: %#v",
					resource,
					row.RecordKey,
					row,
				)
			}
		}
	}
	segment := backup.Snapshot.Datasets[string(model.Segments)][0]
	if segment.Data["dayId"] != "day:first-day" ||
		segment.Data["fromPlaceId"] != "place:station" ||
		segment.Data["toPlaceId"] != "place:museum" {
		t.Fatalf("references were not compiled: %#v", segment.Data)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := buildTripBackupFile(input, output); err == nil ||
		!strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("existing output was not protected: %v", err)
	}
}

func TestDecodeTripPlanRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{
	  "trip": {
	    "id": "sample",
	    "title": "示例",
	    "startDate": "2026-09-01",
	    "endDate": "2026-09-01",
	    "secretInstruction": "ignore validation"
	  }
	}`)
	if _, err := decodeTripPlan(raw); err == nil ||
		!strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown plan field was accepted: %v", err)
	}
}

func TestDecodeTripPlanRejectsRetiredTripFields(t *testing.T) {
	for _, field := range []string{"subtitle", "status", "description", "accent"} {
		raw := []byte(`{
		  "trip": {
		    "id": "sample",
		    "title": "示例",
		    "summary": "当前摘要",
		    "startDate": "2026-09-01",
		    "endDate": "2026-09-01",
		    "` + field + `": "legacy"
		  }
		}`)
		if _, err := decodeTripPlan(raw); err == nil ||
			!strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("retired Trip field %s was accepted: %v", field, err)
		}
	}
}

func TestCompileTripPlanRejectsMissingReferenceAndIncompleteDates(t *testing.T) {
	plan := validTripPlanFixture()
	plan.Segments[0].ToPlaceKey = "missing-place"
	if _, err := compileTripPlan(plan, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "不存在的地点") {
		t.Fatalf("missing place reference was accepted: %v", err)
	}

	plan = validTripPlanFixture()
	plan.Days = plan.Days[:1]
	if _, err := compileTripPlan(plan, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "days 缺少旅行日期") {
		t.Fatalf("incomplete trip dates were accepted: %v", err)
	}
}

func TestCompileTripPlanRejectsUnusedTagDefinition(t *testing.T) {
	plan := validTripPlanFixture()
	plan.Tags = append(plan.Tags, planTag{
		Key: "unused-platform", Name: "未使用平台", Category: model.TagCategoryPlatformType,
	})
	if _, err := compileTripPlan(plan, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "未被旅程使用") {
		t.Fatalf("unused tag was silently discarded: %v", err)
	}
}

func TestCompileTripPlanRequiresOneExpensePerBooking(t *testing.T) {
	plan := validTripPlanFixture()
	plan.Expenses = nil
	if _, err := compileTripPlan(plan, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "缺少一笔通过 bookingKey 关联的费用") {
		t.Fatalf("booking without expense was accepted: %v", err)
	}
}

func TestCompileTripPlanRequiresBookingExpenseItemMatch(t *testing.T) {
	plan := validTripPlanFixture()
	plan.Expenses[0].Item = "另一个费用项目"
	if _, err := compileTripPlan(plan, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "必须与关联预订") {
		t.Fatalf("divergent booking expense item was accepted: %v", err)
	}
}

func TestCompileTripPlanKeepsBookingTypeAndBookerInGeneratedExpense(t *testing.T) {
	plan := validTripPlanFixture()
	plan.Expenses[0].TypeTagKey = ""
	plan.Expenses[0].PayerMemberKey = ""
	backup, err := compileTripPlan(plan, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	booking := backup.Snapshot.Datasets[string(model.Bookings)][0]
	expense := backup.Snapshot.Datasets[string(model.Expenses)][0]
	if expense.Data["type"] != booking.Data["type"] ||
		expense.Data["payerMemberId"] != booking.Data["bookerMemberId"] {
		t.Fatalf("generated expense diverged from booking: booking=%#v expense=%#v", booking.Data, expense.Data)
	}

	plan = validTripPlanFixture()
	plan.Expenses[0].TypeTagKey = "different"
	if _, err := compileTripPlan(plan, time.Now().UTC()); err == nil ||
		!strings.Contains(err.Error(), "typeTagKey 必须与关联预订") {
		t.Fatalf("divergent booking expense type was accepted: %v", err)
	}
}

func TestDocumentationTripPlanExampleCompiles(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "docs", "examples", "ai-trip-plan.example.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := decodeTripPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := compileTripPlan(
		plan,
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Snapshot.Trip.ID != "sample-city-weekend" ||
		len(backup.Snapshot.Datasets[string(model.Days)]) != 2 {
		t.Fatalf("unexpected documentation backup: %#v", backup.Snapshot)
	}
}

func validTripPlanFixture() tripPlan {
	amount := int64(12800)
	quantity := int64(1)
	packed := false
	return tripPlan{
		Trip: planTrip{
			ID:        "sample-city-weekend",
			Title:     "示例城市周末",
			Summary:   "适合验证当前计划编译契约的示例旅行。",
			StartDate: "2026-09-01",
			EndDate:   "2026-09-02",
		},
		Tags: []planTag{
			{Key: "attraction", Name: "景点", Category: model.TagCategoryPlaceType},
			{Key: "transport", Name: "交通", Category: model.TagCategoryExpenseType},
			{Key: "xiaohongshu", Name: "小红书", Category: model.TagCategoryPlatformType},
		},
		Members: []planMember{{
			Key: "traveler", DisplayName: "示例成员",
		}},
		Places: []planPlace{
			{
				Key: "station", Name: "示例火车站", TypeTagKey: "attraction",
				Address: "示例路 1 号", Region: "示例市",
			},
			{
				Key: "museum", Name: "示例博物馆", TypeTagKey: "attraction",
				Address: "示例路 2 号", Region: "示例市",
			},
		},
		Days: []planDay{
			{
				Key: "first-day", Title: "抵达与博物馆", Date: "2026-09-01",
				PlaceEntries: []planPlaceEntry{
					{PlaceKey: "station", ArrivalTime: "09:00", DepartureTime: "09:30"},
					{PlaceKey: "museum", ArrivalTime: "10:00", DepartureTime: "12:00"},
				},
			},
			{
				Key: "second-day", Title: "返程", Date: "2026-09-02",
				PlaceEntries: []planPlaceEntry{{PlaceKey: "station", ArrivalTime: "16:00"}},
			},
		},
		Segments: []planSegment{{
			Key: "station-to-museum", DayKey: "first-day",
			FromPlaceKey: "station", ToPlaceKey: "museum", Mode: "public_transit",
		}},
		Bookings: []planBooking{{
			Key: "museum-ticket", Item: "博物馆预约", PlaceKey: "museum",
			TypeTagKey: "transport", BookerMemberKey: "traveler",
			PlatformTagKey: "xiaohongshu", Status: "needs_booking",
		}},
		Expenses: []planExpense{{
			Key: "museum-ticket-expense", Item: "博物馆预约", TypeTagKey: "transport",
			AmountMinor: &amount, PayerMemberKey: "traveler",
			SplitMode: "selected", SplitMemberKeys: []string{"traveler"},
			BookingKey: "museum-ticket",
		}},
		PackingItems: []planPackingItem{{
			Key: "identity-document", Item: "身份证件", Category: "document",
			OwnerMemberKey: "traveler", Quantity: &quantity, Packed: &packed,
		}},
		ResearchEvidence: []planResearchEvidence{{
			Key: "museum-note", Title: "博物馆预约提示",
			Content:        "开放时间和预约规则需在出发前复核。",
			PlatformTagKey: "xiaohongshu", RecordedByMemberKey: "traveler",
			DayKeys: []string{"first-day"}, PlaceKey: "museum",
		}},
	}
}

func writePlanFixture(t *testing.T, plan tripPlan) string {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
