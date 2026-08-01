package service

import (
	"strings"
	"testing"

	"my-travel/server/internal/model"
)

func TestMongoIntegrationBookingExpenseInheritsTypeAndBooker(t *testing.T) {
	ctx, travelService, repo := authorizationIntegrationService(t)
	owner := createAuthorizationMember(t, ctx, repo, "booking-sync-owner", false)
	other := createAuthorizationMember(t, ctx, repo, "booking-sync-other", false)
	trip := createAuthorizationTrip(t, ctx, repo, "booking-sync-trip", owner.ID)

	for index, member := range []model.LibraryRecord{owner, other} {
		if _, err := repo.CreateRecord(ctx, model.Record{
			TripID:    trip.ID,
			Resource:  model.Members,
			RecordKey: "booking-sync-member-" + member.RecordKey,
			Position:  int64(index),
			Data: map[string]any{
				"memberId": member.ID,
			},
			CreatedByMemberID: owner.ID,
			UpdatedByMemberID: owner.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	types := make([]model.LibraryRecord, 0, 2)
	for _, value := range []struct {
		key  string
		name string
	}{
		{"booking-sync-hotel", "住宿"},
		{"booking-sync-ticket", "门票"},
	} {
		tag, err := repo.CreateLibraryRecord(ctx, model.LibraryRecord{
			Resource:  model.LibraryTags,
			RecordKey: value.key,
			Data: map[string]any{
				"name": value.name, "category": model.TagCategoryExpenseType,
				"status": "active",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, tag)
	}

	booking, _, _, err := travelService.CreateRecord(
		ctx,
		model.Record{
			TripID:    trip.ID,
			Resource:  model.Bookings,
			RecordKey: "booking-sync-booking",
			Data: map[string]any{
				"item": "示例酒店",
				"type": types[0].ID, "bookerMemberId": owner.ID,
			},
		},
		owner.ID,
		"booking-sync-create",
		strings.Repeat("a", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	expense, err := repo.GetExpenseByBooking(ctx, trip.ID, booking.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertBookingExpenseFields(t, expense, "示例酒店", types[0].ID, owner.ID)

	directExpenseEdit := copyAuthorizationData(expense.Data)
	directExpenseEdit["item"] = "绕过来源预订"
	directExpenseEdit["type"] = types[1].ID
	directExpenseEdit["payerMemberId"] = other.ID
	directExpenseEdit["amountMinor"] = int64(88000)
	expense, _, err = travelService.UpdateRecord(
		ctx,
		trip.ID,
		model.Expenses,
		expense.ID,
		expense.Version,
		directExpenseEdit,
		owner.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBookingExpenseFields(t, expense, "示例酒店", types[0].ID, owner.ID)

	bookingEdit := copyAuthorizationData(booking.Data)
	bookingEdit["item"] = "示例景区门票"
	bookingEdit["type"] = types[1].ID
	bookingEdit["bookerMemberId"] = other.ID
	if _, _, err := travelService.UpdateRecord(
		ctx,
		trip.ID,
		model.Bookings,
		booking.ID,
		booking.Version,
		bookingEdit,
		owner.ID,
	); err != nil {
		t.Fatal(err)
	}
	expense, err = repo.GetExpenseByBooking(ctx, trip.ID, booking.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertBookingExpenseFields(t, expense, "示例景区门票", types[1].ID, other.ID)
	if expense.Data["amountMinor"] != int64(88000) {
		t.Fatalf("booking sync unexpectedly discarded editable expense facts: %#v", expense.Data)
	}
}

func assertBookingExpenseFields(
	t *testing.T,
	expense model.Record,
	item, typeID, payerID string,
) {
	t.Helper()
	if expense.Data["item"] != item ||
		expense.Data["type"] != typeID ||
		expense.Data["payerMemberId"] != payerID {
		t.Fatalf(
			"generated expense does not match booking: item=%v type=%v payer=%v",
			expense.Data["item"],
			expense.Data["type"],
			expense.Data["payerMemberId"],
		)
	}
}
