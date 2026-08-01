package model

import "testing"

func TestValidateExpenseRequiresTypeAmountPayerAndFrozenParticipants(t *testing.T) {
	valid := map[string]any{
		"item": "停车费", "type": "tag-expense-type", "amountMinor": int64(1500),
		"payerMemberId": "member-a", "splitMode": "all",
		"splitMemberIds": []any{"member-a", "member-b"},
	}
	if err := ValidateRecordData(Expenses, valid); err != nil {
		t.Fatalf("valid expense rejected: %v", err)
	}
	for name, invalid := range map[string]map[string]any{
		"fractional amount": {
			"item": "停车费", "type": "tag-expense-type", "amountMinor": 15.5,
			"payerMemberId": "member-a", "splitMode": "all", "splitMemberIds": []any{"member-a"},
		},
		"missing type": {
			"item": "停车费", "amountMinor": int64(1500),
			"payerMemberId": "member-a", "splitMode": "all", "splitMemberIds": []any{"member-a"},
		},
		"missing participants": {
			"item": "停车费", "type": "tag-expense-type", "amountMinor": int64(1500),
			"payerMemberId": "member-a", "splitMode": "all", "splitMemberIds": []any{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateRecordData(Expenses, invalid); err == nil {
				t.Fatal("invalid expense accepted")
			}
		})
	}
}

func TestValidateBookingGeneratedExpenseMayWaitForAmount(t *testing.T) {
	candidate := map[string]any{
		"item": "酒店待登记", "type": "tag-expense-type", "bookingRecordId": "booking-a",
		"splitMode": "all", "splitMemberIds": []any{"member-a"},
	}
	if err := ValidateRecordData(Expenses, candidate); err != nil {
		t.Fatalf("booking expense rejected: %v", err)
	}
	delete(candidate, "bookingRecordId")
	if err := ValidateRecordData(Expenses, candidate); err == nil {
		t.Fatal("manual expense without amount and payer accepted")
	}
}

func TestValidateBookingKeepsOnlyReservationFacts(t *testing.T) {
	valid := map[string]any{
		"item":       "景区门票",
		"placeId":    "place-a",
		"platformId": "platform-tag",
		"dateTime":   "2026-09-28 08:00",
		"status":     "booked",
		"link":       "https://example.com/order",
		"notes":      "携带证件",
	}
	if err := ValidateRecordData(Bookings, valid); err != nil {
		t.Fatalf("current booking fields rejected: %v", err)
	}
	if err := ValidateRecordData(Bookings, map[string]any{
		"item": "测试预订", "unsupportedField": "unsupported",
	}); err == nil {
		t.Fatal("unknown booking field accepted")
	}
	if err := ValidateRecordData(Bookings, map[string]any{
		"item": "测试预订", "status": "unsupported",
	}); err == nil {
		t.Fatal("unknown booking status accepted")
	}
}

func TestValidatePackingItemRequiresOwnerMember(t *testing.T) {
	valid := map[string]any{
		"item": "证件", "ownerMemberId": "member-a",
	}
	if err := ValidateRecordData(PackingItems, valid); err != nil {
		t.Fatalf("valid packing item rejected: %v", err)
	}
	delete(valid, "ownerMemberId")
	if err := ValidateRecordData(PackingItems, valid); err == nil {
		t.Fatal("packing item without ownerMemberId accepted")
	}
}

func TestValidateDayDirectlyOwnsOrderedPlaces(t *testing.T) {
	valid := map[string]any{
		"title": "第一天", "date": "2026-09-01",
		"placeEntries": []any{
			map[string]any{"placeId": "place-a", "arrivalTime": "09:00"},
			map[string]any{"placeId": "place-b", "departureTime": "10:30"},
		},
	}
	if err := ValidateRecordData(Days, valid); err != nil {
		t.Fatalf("valid day rejected: %v", err)
	}
	valid["placeEntries"] = []any{
		map[string]any{"placeId": "place-a", "departureTime": "08:00"},
		map[string]any{"placeId": "place-a", "arrivalTime": "18:00"},
	}
	if err := ValidateRecordData(Days, valid); err == nil {
		t.Fatal("duplicate place in one day was accepted")
	}
}

func TestValidateSegmentUsesDistinctPlaces(t *testing.T) {
	valid := map[string]any{
		"dayId": "day-a", "fromPlaceId": "place-a", "toPlaceId": "place-b", "mode": "scenic_bus",
	}
	if err := ValidateRecordData(Segments, valid); err != nil {
		t.Fatalf("valid segment rejected: %v", err)
	}
	valid["toPlaceId"] = "place-a"
	if err := ValidateRecordData(Segments, valid); err == nil {
		t.Fatal("segment with identical endpoints accepted")
	}
	if err := ValidateRecordData(Segments, map[string]any{
		"dayId": "day-a", "fromPlaceId": "place-a", "toPlaceId": "place-b",
		"mode": "drive", "notes": "不受支持字段",
	}); err == nil {
		t.Fatal("unsupported segment notes field accepted")
	}
}

func TestTripIDRejectsPathTraversal(t *testing.T) {
	trip := Trip{ID: "../escape", Title: "x", StartDate: "2026-01-01", EndDate: "2026-01-02"}
	if err := trip.Validate(); err == nil {
		t.Fatal("path-like trip id accepted")
	}
}

func TestTripDateRangeCannotExceedOneYear(t *testing.T) {
	trip := Trip{
		ID: "long-trip", Title: "长途旅行",
		StartDate: "2026-01-01", EndDate: "2027-01-02",
	}
	if err := trip.Validate(); err == nil {
		t.Fatal("trip longer than 366 days accepted")
	}
	trip.EndDate = "2027-01-01"
	if err := trip.Validate(); err != nil {
		t.Fatalf("366-day trip rejected: %v", err)
	}
}

func TestValidateRecordDataRejectsUnknownFields(t *testing.T) {
	for _, field := range []string{"progress", "latestDeparture", "weatherCity", "hardExit"} {
		if err := ValidateRecordData(Days, map[string]any{
			"title": "Day", "date": "2026-09-01", field: "unsupported",
		}); err == nil {
			t.Fatalf("unsupported day field %s accepted", field)
		}
	}
}

func TestValidateResearchEvidenceUsesOnlySimpleCurrentFields(t *testing.T) {
	valid := map[string]any{
		"title":              "景区开放时间",
		"content":            "开放时间以当天公告为准。",
		"platformId":         "platform-tag",
		"recordedByMemberId": "member-a",
		"dayIds":             []string{"day-a"},
		"placeId":            "place-a",
	}
	if err := ValidateRecordData(ResearchEvidence, valid); err != nil {
		t.Fatalf("current research fields rejected: %v", err)
	}
	if err := ValidateRecordData(ResearchEvidence, map[string]any{
		"title": "资料", "unexpectedField": "unsupported",
	}); err == nil {
		t.Fatal("unknown research field was accepted")
	}
}

func TestValidatePlaceKeepsOneManagedTypeAndEssentialFields(t *testing.T) {
	valid := map[string]any{
		"name": "景区入口", "type": "tag-place-type",
		"openingHours": "08:00-17:00", "recommendedTicketPlatform": "官方小程序",
	}
	if err := ValidateRecordData(Places, valid); err != nil {
		t.Fatalf("current place fields rejected: %v", err)
	}
	if err := ValidateRecordData(Places, map[string]any{
		"name": "测试地点", "unexpectedField": "unsupported",
	}); err == nil {
		t.Fatal("unknown place field was accepted")
	}
}

func TestValidateTagRequiresManagedCategory(t *testing.T) {
	for _, category := range []string{
		"general", "place-type", "expense-type", "platform-type",
	} {
		if err := ValidateLibraryRecordData(LibraryTags, map[string]any{
			"name": "标签", "color": "#477566", "status": "active", "category": category,
		}); err != nil {
			t.Fatalf("tag category %s rejected: %v", category, err)
		}
	}
	for _, category := range []string{"", "place"} {
		if err := ValidateLibraryRecordData(LibraryTags, map[string]any{
			"name": "标签", "color": "#477566", "status": "active", "category": category,
		}); err == nil {
			t.Fatalf("invalid tag category %q accepted", category)
		}
	}
}

func TestValidateExpenseAcceptsNamedStringSlice(t *testing.T) {
	type mongoArray []any
	data := map[string]any{
		"item":           "住宿",
		"type":           "tag-expense-type",
		"amountMinor":    int64(10000),
		"payerMemberId":  "member-owner",
		"splitMode":      "all",
		"splitMemberIds": mongoArray{"member-owner"},
	}
	if err := ValidateRecordData(Expenses, data); err != nil {
		t.Fatalf("named string slice rejected: %v", err)
	}
}
