package service

import "testing"

func TestBookingOccurredAtAcceptsDateTimePickerValue(t *testing.T) {
	for input, expected := range map[string]string{
		"2026-09-28T08:30": "2026-09-28T08:30",
		"2026-09-28 08:30": "2026-09-28T08:30",
		"2026-09-28":       "2026-09-28T00:00",
	} {
		if got := bookingOccurredAt(map[string]any{"dateTime": input}); got != expected {
			t.Errorf("bookingOccurredAt(%q)=%q, want %q", input, got, expected)
		}
	}
}

func TestExpenseBookingIDKeepsManualExpensesUnlinked(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"missing": nil,
		"nil":     {"bookingRecordId": nil},
		"empty":   {"bookingRecordId": "  "},
	} {
		t.Run(name, func(t *testing.T) {
			if got := expenseBookingID(data); got != "" {
				t.Fatalf("expenseBookingID()=%q, want empty", got)
			}
		})
	}
	if got := expenseBookingID(map[string]any{
		"bookingRecordId": " booking-a ",
	}); got != "booking-a" {
		t.Fatalf("expenseBookingID()=%q, want booking-a", got)
	}
}
