// 资源校验只约束跨旅行安全与核心字段，不凭空补全旅行事实。
package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var compoundDayPattern = regexp.MustCompile(`(?i)^D\d+\s*-\s*(?:D)?\d+$`)

// ValidateRecordData defines the minimum invariant for every public resource.
// Cross-record references are checked by the service/repository transaction.
func ValidateRecordData(resource Resource, data map[string]any) error {
	if len(data) == 0 {
		return fmt.Errorf("data is required")
	}
	if err := validateAllowedRecordFields(resource, data); err != nil {
		return err
	}
	required := map[Resource]string{
		Days:             "title",
		Places:           "name",
		Segments:         "dayId",
		Bookings:         "item",
		Expenses:         "item",
		PackingItems:     "item",
		ResearchEvidence: "title",
		Members:          "memberId",
	}
	key, ok := required[resource]
	if !ok {
		return fmt.Errorf("unknown resource: %s", resource)
	}
	if blank(data[key]) {
		return fmt.Errorf("%s is required", key)
	}
	if resource == PackingItems && blank(data["ownerMemberId"]) {
		return fmt.Errorf("ownerMemberId is required")
	}

	switch resource {
	case Days:
		var day DayData
		if err := decodeCoreData(data, &day); err != nil {
			return err
		}
		if !validDate(day.Date) {
			return fmt.Errorf("date must be YYYY-MM-DD")
		}
		places := make(map[string]struct{}, len(day.PlaceEntries))
		for _, entry := range day.PlaceEntries {
			if strings.TrimSpace(entry.PlaceID) == "" {
				return fmt.Errorf("placeEntries.placeId is required")
			}
			if _, duplicate := places[entry.PlaceID]; duplicate {
				return fmt.Errorf("placeEntries must not contain duplicate placeId")
			}
			places[entry.PlaceID] = struct{}{}
			for _, value := range []string{entry.ArrivalTime, entry.DepartureTime} {
				if value != "" && !validClockTime(value) {
					return fmt.Errorf("placeEntries time must be HH:MM")
				}
			}
		}
	case Places:
		var place PlaceData
		if err := decodeCoreData(data, &place); err != nil {
			return err
		}
		if place.WeatherAdcode != "" && !validAdcode(place.WeatherAdcode) {
			return fmt.Errorf("weatherAdcode must contain exactly 6 digits")
		}
		if (place.Latitude == nil) != (place.Longitude == nil) {
			return fmt.Errorf("latitude and longitude must be provided together")
		}
		if place.Latitude != nil && (*place.Latitude < -90 || *place.Latitude > 90) {
			return fmt.Errorf("latitude is out of range")
		}
		if place.Longitude != nil && (*place.Longitude < -180 || *place.Longitude > 180) {
			return fmt.Errorf("longitude is out of range")
		}
	case Segments:
		var segment SegmentData
		if err := decodeCoreData(data, &segment); err != nil {
			return err
		}
		if strings.TrimSpace(segment.DayID) == "" ||
			strings.TrimSpace(segment.FromPlaceID) == "" ||
			strings.TrimSpace(segment.ToPlaceID) == "" {
			return fmt.Errorf("dayId, fromPlaceId and toPlaceId are required")
		}
		if compoundReference(segment.DayID) {
			return fmt.Errorf("dayId must reference exactly one day record")
		}
		if segment.FromPlaceID == segment.ToPlaceID {
			return fmt.Errorf("segment endpoints must be different places")
		}
		if !validSegmentMode(segment.Mode) {
			return fmt.Errorf("invalid segment mode")
		}
		if segment.DurationMinutes != nil && *segment.DurationMinutes < 0 {
			return fmt.Errorf("durationMinutes cannot be negative")
		}
		if segment.DistanceMeters != nil && *segment.DistanceMeters < 0 {
			return fmt.Errorf("distanceMeters cannot be negative")
		}
	case Expenses:
		linkedBooking := !blank(data["bookingRecordId"])
		if !blank(data["amountMinor"]) {
			amount, ok := integer(data["amountMinor"])
			if !ok || amount <= 0 {
				return fmt.Errorf("amountMinor must be a positive integer")
			}
		} else if !linkedBooking {
			return fmt.Errorf("amountMinor must be a positive integer")
		}
		if blank(data["payerMemberId"]) && !linkedBooking {
			return fmt.Errorf("payerMemberId is required")
		}
		mode := fmt.Sprint(data["splitMode"])
		switch mode {
		case "selected", "all":
		default:
			return fmt.Errorf("invalid splitMode")
		}
		participants, _ := strictStringSlice(data["splitMemberIds"])
		if mode == "" || len(participants) == 0 {
			return fmt.Errorf("splitMode and frozen splitMemberIds are required")
		}
		if blank(data["type"]) {
			return fmt.Errorf("type is required")
		}
	}
	return nil
}

func decodeCoreData(data map[string]any, target any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("data cannot be encoded: %w", err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("data has invalid field types: %w", err)
	}
	return nil
}

func validSegmentMode(mode string) bool {
	switch mode {
	case "walk", "drive", "taxi", "rideshare", "public_transit", "train", "flight",
		"shuttle", "scenic_bus", "ferry", "bicycle", "other":
		return true
	default:
		return false
	}
}

func compoundReference(value string) bool {
	return strings.ContainsAny(value, "/,") ||
		strings.Contains(value, "..") ||
		compoundDayPattern.MatchString(strings.TrimSpace(value))
}

func blank(value any) bool {
	return value == nil || strings.TrimSpace(fmt.Sprint(value)) == ""
}

func integer(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if number == float64(int64(number)) {
			return int64(number), true
		}
	}
	return 0, false
}

func validDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func validAdcode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
