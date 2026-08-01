// 本文件声明当前资源唯一允许的数据字段与稳定枚举。
package model

import (
	"fmt"
	"net/url"
	"reflect"
	"time"
)

var allowedRecordFields = map[Resource]map[string]struct{}{
	Days: fieldSet(
		"title", "date", "placeEntries", "additionalInfo",
	),
	Places: fieldSet(
		"name", "type", "address", "region", "latitude", "longitude", "weatherAdcode",
		"navigationURL", "openingHours",
		"recommendedTicketPlatform",
	),
	Segments: fieldSet(
		"mode", "dayId", "fromPlaceId", "toPlaceId", "departureTime", "durationMinutes",
		"distanceMeters", "navigationURL", "additionalInfo",
	),
	Bookings: fieldSet(
		"item", "type", "bookerMemberId", "placeId", "platformId", "dateTime",
		"status", "link", "notes",
	),
	Expenses: fieldSet(
		"item", "occurredAt", "type", "amountMinor", "payerMemberId", "splitMode",
		"splitMemberIds", "bookingRecordId", "notes",
	),
	PackingItems: fieldSet(
		"item", "category", "dayIds", "ownerMemberId", "quantity", "packed", "notes",
	),
	ResearchEvidence: fieldSet(
		"title", "content", "platformId", "recordedByMemberId", "dayIds", "placeId",
	),
	Members: fieldSet("memberId", "role", "note"),
}

func validateAllowedRecordFields(resource Resource, data map[string]any) error {
	allowed, ok := allowedRecordFields[resource]
	if !ok {
		return fmt.Errorf("unknown resource: %s", resource)
	}
	for field := range data {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%s is not allowed for %s", field, resource)
		}
	}

	if err := requireStringFields(data, stringFields(resource)...); err != nil {
		return err
	}
	for _, field := range []string{"dayIds", "splitMemberIds"} {
		if value, exists := data[field]; exists {
			values, err := strictStringSlice(value)
			if err != nil {
				return fmt.Errorf("%s must be an array of strings", field)
			}
			if field == "dayIds" || field == "splitMemberIds" {
				for _, id := range values {
					if err := ValidateRecordID(id); err != nil {
						return fmt.Errorf("%s contains an invalid record id", field)
					}
					if field == "dayIds" && compoundReference(id) {
						return fmt.Errorf("dayIds must contain individual day record ids")
					}
				}
			}
		}
	}
	for _, field := range []string{
		"type", "platformId", "dayId", "placeId", "fromPlaceId", "toPlaceId",
		"ownerMemberId", "payerMemberId", "bookerMemberId", "recordedByMemberId",
		"bookingRecordId",
	} {
		if value, exists := data[field]; exists && !blank(value) {
			text, ok := value.(string)
			if !ok || ValidateRecordID(text) != nil {
				return fmt.Errorf("%s must be a valid record id", field)
			}
		}
	}
	for _, field := range []string{"navigationURL", "link"} {
		if value, exists := data[field]; exists && !blank(value) {
			text, ok := value.(string)
			if !ok || !validHTTPURL(text) {
				return fmt.Errorf("%s must be an http or https URL", field)
			}
		}
	}
	for _, field := range []string{"departureTime"} {
		if value, exists := data[field]; exists && !blank(value) {
			text, ok := value.(string)
			if !ok || !validClockTime(text) {
				return fmt.Errorf("%s must be HH:MM", field)
			}
		}
	}
	for _, field := range []string{"durationMinutes", "distanceMeters", "quantity"} {
		if value, exists := data[field]; exists {
			number, ok := integer(value)
			if !ok || number < 0 {
				return fmt.Errorf("%s must be a non-negative integer", field)
			}
		}
	}
	for _, field := range []string{"packed"} {
		if value, exists := data[field]; exists {
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", field)
			}
		}
	}
	if status, exists := data["status"]; exists && !blank(status) {
		value, ok := status.(string)
		if !ok || !validStatus(resource, value) {
			return fmt.Errorf("invalid status")
		}
	}
	return validateResourceEnums(resource, data)
}

func validateResourceEnums(resource Resource, data map[string]any) error {
	switch resource {
	case Expenses:
		if value, ok := data["splitMode"]; ok && !blank(value) &&
			!oneOf(value, "all", "selected") {
			return fmt.Errorf("invalid splitMode")
		}
	case PackingItems:
		if value, ok := data["category"]; ok && !blank(value) &&
			!oneOf(
				value,
				"clothing", "document", "medicine", "electronics", "daily_use",
				"supply", "gear", "vehicle", "other",
			) {
			return fmt.Errorf("invalid packing category")
		}
	}
	return nil
}

func stringFields(resource Resource) []string {
	switch resource {
	case Days:
		return []string{"title", "date", "additionalInfo"}
	case Places:
		return []string{"name", "type", "address", "region", "weatherAdcode", "navigationURL", "openingHours", "recommendedTicketPlatform"}
	case Segments:
		return []string{"mode", "dayId", "fromPlaceId", "toPlaceId", "departureTime", "navigationURL", "additionalInfo"}
	case Bookings:
		return []string{
			"item", "type", "bookerMemberId", "placeId", "platformId",
			"dateTime", "status", "link", "notes",
		}
	case Expenses:
		return []string{"item", "occurredAt", "type", "payerMemberId", "splitMode", "bookingRecordId", "notes"}
	case PackingItems:
		return []string{"item", "category", "ownerMemberId", "notes"}
	case ResearchEvidence:
		return []string{"title", "content", "platformId", "recordedByMemberId", "placeId"}
	case Members:
		return []string{"memberId", "role", "note"}
	default:
		return nil
	}
}

func requireStringFields(data map[string]any, fields ...string) error {
	for _, field := range fields {
		if value, exists := data[field]; exists {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", field)
			}
		}
	}
	return nil
}

func strictStringSlice(value any) ([]string, error) {
	switch values := value.(type) {
	case []string:
		return values, nil
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("array item is not a string")
			}
			result = append(result, text)
		}
		return result, nil
	}
	array := reflect.ValueOf(value)
	if !array.IsValid() || (array.Kind() != reflect.Array && array.Kind() != reflect.Slice) {
		return nil, fmt.Errorf("value is not an array")
	}
	result := make([]string, 0, array.Len())
	for index := 0; index < array.Len(); index++ {
		text, ok := array.Index(index).Interface().(string)
		if !ok {
			return nil, fmt.Errorf("array item is not a string")
		}
		result = append(result, text)
	}
	return result, nil
}

func validStatus(resource Resource, value string) bool {
	if resource == Bookings {
		return oneOf(value, "needs_booking", "booked", "cancelled")
	}
	return oneOf(
		value,
		"planned", "needs_booking", "booked", "confirmed", "in_progress",
		"completed", "skipped", "cancelled",
	)
}

func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func validClockTime(value string) bool {
	_, err := time.Parse("15:04", value)
	return err == nil
}

func oneOf(value any, allowed ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if text == candidate {
			return true
		}
	}
	return false
}

func fieldSet(fields ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		result[field] = struct{}{}
	}
	return result
}
