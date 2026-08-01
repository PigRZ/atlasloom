// 领域模型定义 Trip 聚合根、旅行资源、会话和完整快照结构。
package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Resource names are stable public API and storage identifiers.
type Resource string

const (
	Days             Resource = "days"
	Places           Resource = "places"
	Segments         Resource = "segments"
	Bookings         Resource = "bookings"
	Expenses         Resource = "expenses"
	PackingItems     Resource = "packing-items"
	ResearchEvidence Resource = "research-evidence"
	Members          Resource = "members"
)

func ParseResource(value string) (Resource, error) {
	resource := Resource(value)
	for _, candidate := range Resources() {
		if resource == candidate {
			return resource, nil
		}
	}
	return "", fmt.Errorf("unknown resource: %s", value)
}

func Resources() []Resource {
	return []Resource{
		Days,
		Places,
		Segments,
		Bookings,
		Expenses,
		PackingItems,
		ResearchEvidence,
		Members,
	}
}

var tripIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
var recordIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func ValidateRecordID(value string) error {
	if !recordIdentifierPattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("record id must be a URL-safe identifier of at most 128 characters")
	}
	return nil
}

func ValidateRecordKey(value string) error {
	if !recordIdentifierPattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("recordKey must be a stable URL-safe key of at most 128 characters")
	}
	return nil
}

type Trip struct {
	ID                string     `bson:"_id" json:"id"`
	Title             string     `bson:"title" json:"title"`
	Summary           string     `bson:"summary" json:"summary"`
	StartDate         string     `bson:"start_date" json:"startDate"`
	EndDate           string     `bson:"end_date" json:"endDate"`
	Timezone          string     `bson:"timezone" json:"timezone"`
	Currency          string     `bson:"currency" json:"currency"`
	Version           int64      `bson:"version" json:"version"`
	CreatedAt         time.Time  `bson:"created_at" json:"createdAt"`
	UpdatedAt         time.Time  `bson:"updated_at" json:"updatedAt"`
	DeletedAt         *time.Time `bson:"deleted_at,omitempty" json:"deletedAt,omitempty"`
	DeletedByMemberID string     `bson:"deleted_by_member_id,omitempty" json:"deletedByMemberId,omitempty"`
}

func (t Trip) Validate() error {
	if !tripIDPattern.MatchString(t.ID) {
		return fmt.Errorf("trip id must be a lowercase slug containing only letters, digits, and hyphens")
	}
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("trip title is required")
	}
	start, err := time.Parse("2006-01-02", t.StartDate)
	if err != nil {
		return fmt.Errorf("startDate must be YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", t.EndDate)
	if err != nil {
		return fmt.Errorf("endDate must be YYYY-MM-DD")
	}
	if t.EndDate < t.StartDate {
		return fmt.Errorf("endDate cannot be before startDate")
	}
	if end.Sub(start) > 365*24*time.Hour {
		return fmt.Errorf("trip date range cannot exceed 366 days")
	}
	return nil
}

// DayPlaceEntry is stored inline on Day and has no independent ID or lifecycle.
type DayPlaceEntry struct {
	PlaceID        string `json:"placeId"`
	ArrivalTime    string `json:"arrivalTime,omitempty"`
	DepartureTime  string `json:"departureTime,omitempty"`
	AdditionalInfo string `json:"additionalInfo,omitempty"`
}

type DayData struct {
	Title          string          `json:"title"`
	Date           string          `json:"date"`
	PlaceEntries   []DayPlaceEntry `json:"placeEntries,omitempty"`
	AdditionalInfo string          `json:"additionalInfo,omitempty"`
}

type PlaceData struct {
	Name                      string   `json:"name"`
	Type                      string   `json:"type"`
	Address                   string   `json:"address,omitempty"`
	Region                    string   `json:"region,omitempty"`
	Latitude                  *float64 `json:"latitude,omitempty"`
	Longitude                 *float64 `json:"longitude,omitempty"`
	WeatherAdcode             string   `json:"weatherAdcode,omitempty"`
	NavigationURL             string   `json:"navigationURL,omitempty"`
	OpeningHours              string   `json:"openingHours,omitempty"`
	RecommendedTicketPlatform string   `json:"recommendedTicketPlatform,omitempty"`
}

type SegmentData struct {
	DayID           string `json:"dayId"`
	FromPlaceID     string `json:"fromPlaceId"`
	ToPlaceID       string `json:"toPlaceId"`
	Mode            string `json:"mode"`
	DurationMinutes *int64 `json:"durationMinutes,omitempty"`
	DistanceMeters  *int64 `json:"distanceMeters,omitempty"`
	AdditionalInfo  string `json:"additionalInfo,omitempty"`
}

type AuditLog struct {
	ID          string         `bson:"_id" json:"id"`
	TripID      string         `bson:"trip_id,omitempty" json:"tripId,omitempty"`
	MemberID    string         `bson:"member_id" json:"memberId"`
	Module      string         `bson:"module" json:"module"`
	Action      string         `bson:"action" json:"action"`
	Resource    string         `bson:"resource,omitempty" json:"resource,omitempty"`
	RecordID    string         `bson:"record_id,omitempty" json:"recordId,omitempty"`
	RecordLabel string         `bson:"record_label,omitempty" json:"recordLabel,omitempty"`
	Before      map[string]any `bson:"before,omitempty" json:"before,omitempty"`
	After       map[string]any `bson:"after,omitempty" json:"after,omitempty"`
	CreatedAt   time.Time      `bson:"created_at" json:"createdAt"`
}

type Record struct {
	ID                string         `bson:"_id" json:"id"`
	TripID            string         `bson:"trip_id" json:"tripId"`
	Resource          Resource       `bson:"resource" json:"resource"`
	RecordKey         string         `bson:"record_key" json:"recordKey"`
	GroupKey          string         `bson:"group_key" json:"-"`
	Position          int64          `bson:"position" json:"position"`
	Version           int64          `bson:"version" json:"version"`
	Data              map[string]any `bson:"data" json:"data"`
	CreatedByMemberID string         `bson:"created_by_member_id" json:"createdByMemberId"`
	UpdatedByMemberID string         `bson:"updated_by_member_id" json:"updatedByMemberId"`
	CreatedAt         time.Time      `bson:"created_at" json:"createdAt"`
	UpdatedAt         time.Time      `bson:"updated_at" json:"updatedAt"`
}

// MemberIdentity is the authenticated projection of one base-library member.
// Its ID is always the member record ID; login never introduces another domain ID.
type MemberIdentity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Mobile      string `json:"mobile"`
	Admin       bool   `json:"admin"`
}

// MemberCredential is private authentication material keyed by the member ID.
// It is deliberately excluded from library records and snapshots.
type MemberCredential struct {
	MemberID     string `bson:"_id"`
	PasswordHash string `bson:"password_hash"`
}

type Membership struct {
	ID        string    `bson:"_id" json:"id"`
	TripID    string    `bson:"trip_id" json:"tripId"`
	MemberID  string    `bson:"member_id" json:"memberId"`
	Role      string    `bson:"role" json:"role"`
	CreatedAt time.Time `bson:"created_at" json:"createdAt"`
}

type Session struct {
	ID        string    `bson:"_id" json:"-"`
	MemberID  string    `bson:"member_id" json:"memberId"`
	CSRFToken string    `bson:"csrf_token" json:"csrfToken"`
	ExpiresAt time.Time `bson:"expires_at" json:"expiresAt"`
}

type Snapshot struct {
	ID              string    `bson:"_id" json:"id"`
	TripID          string    `bson:"trip_id" json:"tripId"`
	Revision        int64     `bson:"revision" json:"revision"`
	Reason          string    `bson:"reason" json:"reason"`
	Payload         []byte    `bson:"payload" json:"-"`
	SHA256          string    `bson:"sha256" json:"sha256"`
	ContentChecksum string    `bson:"content_checksum" json:"contentChecksum"`
	MirrorState     string    `bson:"mirror_state" json:"mirrorState"`
	CreatedAt       time.Time `bson:"created_at" json:"createdAt"`
}

// SnapshotPayload is the portable data contract used by download, preview and
// two-phase import. ContentChecksum deliberately excludes volatile export
// metadata, so the same logical revision has one stable checksum.
type SnapshotPayload struct {
	ExportedAt      time.Time           `json:"exportedAt"`
	Reason          string              `json:"reason"`
	Revision        int64               `json:"revision"`
	ContentChecksum string              `json:"contentChecksum"`
	Trip            Trip                `json:"trip"`
	Datasets        map[string][]Record `json:"datasets"`
}

func (p SnapshotPayload) ComputedContentChecksum() (string, error) {
	content := struct {
		Revision int64               `json:"revision"`
		Trip     Trip                `json:"trip"`
		Datasets map[string][]Record `json:"datasets"`
	}{p.Revision, p.Trip, p.Datasets}
	return canonicalJSONChecksum(content)
}

// SameJSONMeaning compares JSON-compatible values after normalizing BSON
// containers, object key order, and equivalent numeric representations.
func SameJSONMeaning(left, right any) bool {
	leftPayload, leftErr := canonicalJSON(left)
	rightPayload, rightErr := canonicalJSON(right)
	return leftErr == nil &&
		rightErr == nil &&
		bytes.Equal(leftPayload, rightPayload)
}

func canonicalJSON(content any) ([]byte, error) {
	payload, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func canonicalJSONChecksum(content any) (string, error) {
	payload, err := canonicalJSON(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type IdempotencyRequest struct {
	ID          string    `bson:"_id"`
	Scope       string    `bson:"scope"`
	Key         string    `bson:"key"`
	RequestHash string    `bson:"request_hash"`
	Response    []byte    `bson:"response"`
	CreatedAt   time.Time `bson:"created_at"`
	ExpiresAt   time.Time `bson:"expires_at"`
}
