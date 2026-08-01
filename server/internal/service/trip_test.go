// 旅行测试固定逻辑删除在操作记录中的可追溯字段。
package service

import (
	"testing"
	"time"

	"my-travel/server/internal/model"
)

func TestInitialTripDaysCoversEveryCalendarDate(t *testing.T) {
	days, err := initialTripDays(model.Trip{
		ID: "trip-test", StartDate: "2026-09-29", EndDate: "2026-10-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDates := []string{"2026-09-29", "2026-09-30", "2026-10-01"}
	wantKeys := []string{"day-20260929", "day-20260930", "day-20261001"}
	if len(days) != len(wantDates) {
		t.Fatalf("day count=%d, want %d", len(days), len(wantDates))
	}
	for index, want := range wantDates {
		if got := days[index].Data["date"]; got != want {
			t.Fatalf("day %d date=%v, want %s", index, got, want)
		}
		if days[index].RecordKey != wantKeys[index] {
			t.Fatalf(
				"day %d record key=%q, want %q",
				index,
				days[index].RecordKey,
				wantKeys[index],
			)
		}
	}
}

func TestTripAuditDataIncludesLogicalDeletion(t *testing.T) {
	deletedAt := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	data := tripAuditData(model.Trip{
		Title: "测试旅行", StartDate: "2026-09-01", EndDate: "2026-09-02",
		Summary:   "保留的旅行摘要",
		DeletedAt: &deletedAt, DeletedByMemberID: "member-owner",
	})
	if data["deletedAt"] != "2026-07-27T10:00:00Z" ||
		data["deletedByMemberId"] != "member-owner" ||
		data["summary"] != "保留的旅行摘要" {
		t.Fatalf("logical deletion audit fields missing: %#v", data)
	}
	for _, retired := range []string{"subtitle", "status", "description", "accent"} {
		if _, ok := data[retired]; ok {
			t.Fatalf("retired Trip field %s leaked into audit data: %#v", retired, data)
		}
	}
}
