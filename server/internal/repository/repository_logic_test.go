// 这些纯逻辑测试固定跨日移动、快照身份与语义差异。
package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"my-travel/server/internal/model"
)

func TestActiveTripFilterExcludesLogicallyDeletedTrips(t *testing.T) {
	filter := activeTripFilter("trip-a")
	if filter["_id"] != "trip-a" {
		t.Fatalf("trip id missing from filter: %#v", filter)
	}
	deletedAt, ok := filter["deleted_at"].(bson.M)
	if !ok || deletedAt["$exists"] != false {
		t.Fatalf("logical deletion filter missing: %#v", filter)
	}
}

func TestMemberAuthenticationDisabledOnlyForExplicitInactiveStatus(t *testing.T) {
	for name, test := range map[string]struct {
		data map[string]any
		want bool
	}{
		"missing status defaults to active": {data: map[string]any{}, want: false},
		"nil status defaults to active":     {data: map[string]any{"status": nil}, want: false},
		"active member":                     {data: map[string]any{"status": "active"}, want: false},
		"inactive member":                   {data: map[string]any{"status": "inactive"}, want: true},
		"inactive member with whitespace":   {data: map[string]any{"status": " inactive "}, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			row := model.LibraryRecord{Data: test.data}
			if got := memberAuthenticationDisabled(row); got != test.want {
				t.Fatalf("memberAuthenticationDisabled()=%v want=%v", got, test.want)
			}
		})
	}
}

func TestTargetGroupForUpdateDetectsSegmentDayMove(t *testing.T) {
	current := model.Record{Resource: model.Segments, GroupKey: "day-a", Position: 0}
	target, moved := targetGroupForUpdate(current, map[string]any{
		"dayId": "day-b", "fromPlaceId": "place-a", "toPlaceId": "place-b", "mode": "drive",
	})
	if target != "day-b" || !moved {
		t.Fatalf("target=%q moved=%v", target, moved)
	}

	target, moved = targetGroupForUpdate(current, map[string]any{
		"dayId": "day-a", "fromPlaceId": "place-a", "toPlaceId": "place-c", "mode": "drive",
	})
	if target != "day-a" || moved {
		t.Fatalf("same-day update target=%q moved=%v", target, moved)
	}
}

func TestDayPlaceEntriesAcceptsMongoOrderedDocuments(t *testing.T) {
	entries := dayPlaceEntries(bson.A{
		bson.D{
			{Key: "placeId", Value: "place-a"},
			{Key: "arrivalTime", Value: "09:00"},
		},
	})
	if len(entries) != 1 || entries[0]["placeId"] != "place-a" {
		t.Fatalf("Mongo ordered document was not decoded: %#v", entries)
	}
}

func TestDiffTripMetadataReportsEveryEditableField(t *testing.T) {
	current := model.Trip{
		Title: "Old", Summary: "Old summary",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
		Timezone: "Asia/Shanghai", Currency: "CNY",
	}
	incoming := model.Trip{
		Title: "New", Summary: "New summary",
		StartDate: "2026-10-01", EndDate: "2026-10-03",
		Timezone: "Asia/Tokyo", Currency: "JPY",
	}
	changes := DiffTripMetadata(current, incoming)
	for _, field := range []string{
		"title", "summary", "startDate", "endDate", "timezone", "currency",
	} {
		if _, ok := changes[field]; !ok {
			t.Fatalf("editable Trip field %s was omitted: %#v", field, changes)
		}
	}
	if len(changes) != 6 {
		t.Fatalf("unexpected Trip changes: %#v", changes)
	}
	if got := DiffTripMetadata(current, current); len(got) != 0 {
		t.Fatalf("unchanged Trip reported differences: %#v", got)
	}
}

func TestValidatedTripPatchUsesOnlyCurrentMetadata(t *testing.T) {
	current := model.Trip{
		ID: "trip-current", Title: "当前旅行", Summary: "旧摘要",
		StartDate: "2026-09-01", EndDate: "2026-09-02",
		Timezone: "Asia/Shanghai", Currency: "CNY",
	}
	set, err := validatedTripPatch(current, map[string]any{"summary": "新摘要"})
	if err != nil {
		t.Fatal(err)
	}
	if set["summary"] != "新摘要" {
		t.Fatalf("summary patch was not mapped to MongoDB: %#v", set)
	}
	for _, retired := range []string{"subtitle", "status", "description", "accent"} {
		if _, err := validatedTripPatch(current, map[string]any{retired: "legacy"}); err == nil ||
			!strings.Contains(err.Error(), "is not editable") {
			t.Fatalf("retired Trip field %s was accepted: %v", retired, err)
		}
	}
}

func TestValidateTripDayBoundsRejectsExcludedDays(t *testing.T) {
	days := []model.Record{
		{Data: map[string]any{"date": "2026-09-25"}},
		{Data: map[string]any{"date": "2026-09-27"}},
	}
	if err := validateTripDayBounds("2026-09-25", "2026-09-27", days); err != nil {
		t.Fatalf("valid trip range was rejected: %v", err)
	}
	if err := validateTripDayBounds("2026-09-26", "2026-09-27", days); err == nil {
		t.Fatal("trip range excluding an existing day was accepted")
	}
}

func TestValidateRecordIdentityTransitionRejectsIDReuse(t *testing.T) {
	current := emptyRecordDatasets()
	current["days"] = []model.Record{{
		ID: "stable-id", Resource: model.Days, RecordKey: "day-a",
	}}

	for name, incomingRow := range map[string]model.Record{
		"new key": {
			ID: "stable-id", Resource: model.Days, RecordKey: "day-b",
		},
		"new resource": {
			ID: "stable-id", Resource: model.Places, RecordKey: "place-a",
		},
	} {
		t.Run(name, func(t *testing.T) {
			incoming := emptyRecordDatasets()
			incoming[string(incomingRow.Resource)] = []model.Record{incomingRow}
			if err := ValidateRecordIdentityTransition(current, incoming); err == nil ||
				!strings.Contains(err.Error(), ErrSnapshotIdentity.Error()) {
				t.Fatalf("ID reuse was accepted: %v", err)
			}
		})
	}

	unchanged := emptyRecordDatasets()
	unchanged["days"] = []model.Record{{
		ID: "stable-id", Resource: model.Days, RecordKey: "day-a",
	}}
	if err := ValidateRecordIdentityTransition(current, unchanged); err != nil {
		t.Fatalf("stable identity was rejected: %v", err)
	}
}

func TestBuildMembershipAccessDiffIsStableAndPreservesExistingRoles(t *testing.T) {
	diff := buildMembershipAccessDiff(
		map[string]struct{}{
			"member-owner": {},
			"member-a":     {},
			"member-d":     {},
			"member-c":     {},
		},
		map[string]model.Membership{
			"member-z":     {MemberID: "member-z", Role: "viewer"},
			"member-owner": {MemberID: "member-owner", Role: "owner"},
			"member-b":     {MemberID: "member-b", Role: "editor"},
			"member-a":     {MemberID: "member-a", Role: "viewer"},
		},
		"member-owner",
	)
	raw, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"created":[{"memberId":"member-c","role":"editor"},{"memberId":"member-d","role":"editor"}],"deleted":[{"memberId":"member-b","role":"editor"},{"memberId":"member-z","role":"viewer"}]}`
	if string(raw) != want {
		t.Fatalf("membership access diff=%s, want %s", raw, want)
	}
}

func TestRestorePreviewIncludesCompleteSemanticDiff(t *testing.T) {
	diff := SnapshotDiff{
		BaseRevision: 2, CurrentRevision: 5, Checksum: strings.Repeat("a", 64),
		TripChanges: map[string]TripFieldDiff{
			"title": {Current: "Current", Incoming: "Target"},
		},
		Resources: map[string]ResourceDiff{
			"places": {Current: 2, Incoming: 1, Deleted: 1},
		},
		MembershipAccess: MembershipAccessDiff{
			Created: []MembershipAccessChange{},
			Deleted: []MembershipAccessChange{
				{MemberID: "member-b", Role: "viewer"},
			},
		},
		DeleteCount: 1,
	}
	preview := newRestorePreview(
		model.Snapshot{ID: "snapshot-a", SHA256: strings.Repeat("b", 64)},
		model.SnapshotPayload{},
		diff,
	)
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{
		`"tripChanges"`, `"resources"`, `"deleteCount":1`,
		`"membershipAccess":{"created":[],"deleted":[{"memberId":"member-b","role":"viewer"}]}`,
		`"baseRevision":2`, `"currentRevision":5`,
	} {
		if !strings.Contains(text, field) {
			t.Fatalf("restore preview omitted %s: %s", field, text)
		}
	}
	if strings.Contains(text, "recordCounts") {
		t.Fatalf("count-only preview field leaked into response: %s", text)
	}
}

func TestSnapshotDiffFinalizeDistinguishesNoOpFromMutation(t *testing.T) {
	noOp := SnapshotDiff{
		TripChanges: map[string]TripFieldDiff{},
		Resources: map[string]ResourceDiff{
			"days": {Current: 1, Incoming: 1},
		},
	}
	noOp.Finalize()
	if noOp.HasChanges || noOp.Message != SnapshotNoChangesMessage {
		t.Fatalf("zero diff was not recognized as a no-op: %#v", noOp)
	}

	changed := noOp
	changed.Resources = map[string]ResourceDiff{
		"days": {Current: 1, Incoming: 1, Updated: 1},
	}
	changed.Finalize()
	if !changed.HasChanges || changed.Message != SnapshotChangesMessage {
		t.Fatalf("mutation diff was not recognized: %#v", changed)
	}

	membershipOnly := noOp
	membershipOnly.MembershipAccess = MembershipAccessDiff{
		Created: []MembershipAccessChange{
			{MemberID: "member-b", Role: "editor"},
		},
		Deleted: []MembershipAccessChange{},
	}
	membershipOnly.Finalize()
	if !membershipOnly.HasChanges ||
		membershipOnly.Message != SnapshotChangesMessage {
		t.Fatalf("membership-only diff was not recognized: %#v", membershipOnly)
	}
}

func TestLibraryReferenceFiltersCoverEveryBaseLibraryResource(t *testing.T) {
	tests := []struct {
		resource model.LibraryResource
	}{
		{model.LibraryMembers},
		{model.LibraryTags},
	}
	for _, test := range tests {
		filter := libraryReferenceFilter(model.LibraryRecord{
			ID:       "library-item",
			Resource: test.resource,
		})
		if _, ok := filter["$or"]; !ok {
			t.Fatalf("%s deletion does not check every reference: %#v", test.resource, filter)
		}
	}
}

func TestLibraryReferenceFiltersIncludePlatformAndResearchRecorder(t *testing.T) {
	memberFilter := fmt.Sprint(libraryReferenceFilter(model.LibraryRecord{
		ID: "member-a", Resource: model.LibraryMembers,
	}))
	for _, expected := range []string{
		"bookerMemberId",
		"recordedByMemberId",
		"payerMemberId",
		"splitMemberIds",
		"ownerMemberId",
	} {
		if !strings.Contains(memberFilter, expected) {
			t.Fatalf("member reference filter misses %s: %s", expected, memberFilter)
		}
	}
	tagFilter := fmt.Sprint(libraryReferenceFilter(model.LibraryRecord{
		ID: "platform-a", Resource: model.LibraryTags,
	}))
	for _, expected := range []string{"platformId", "data.type"} {
		if !strings.Contains(tagFilter, expected) {
			t.Fatalf("tag reference filter misses %s: %s", expected, tagFilter)
		}
	}
}

func TestLibraryReferenceRelationsExplainEveryMemberUse(t *testing.T) {
	member := model.LibraryRecord{
		ID: "member-a", Resource: model.LibraryMembers,
	}
	expenseRelations := strings.Join(libraryReferenceRelations(member, model.Record{
		Resource: model.Expenses,
		Data: map[string]any{
			"payerMemberId":  "member-a",
			"splitMemberIds": []string{"member-a"},
		},
	}), ",")
	if expenseRelations != "expense-payer,expense-split" {
		t.Fatalf("expense relations=%q", expenseRelations)
	}
	bookingRelations := strings.Join(libraryReferenceRelations(member, model.Record{
		Resource: model.Bookings,
		Data:     map[string]any{"bookerMemberId": "member-a"},
	}), ",")
	if bookingRelations != "booking-booker" {
		t.Fatalf("booking relations=%q", bookingRelations)
	}
	packingRelations := strings.Join(libraryReferenceRelations(member, model.Record{
		Resource: model.PackingItems,
		Data:     map[string]any{"ownerMemberId": "member-a"},
	}), ",")
	if packingRelations != "packing-owner" {
		t.Fatalf("packing relations=%q", packingRelations)
	}
}

func TestLibraryReferenceLabelUsesVisibleBusinessName(t *testing.T) {
	row := model.Record{
		RecordKey: "technical-key",
		Data:      map[string]any{"item": "示例景区门票"},
	}
	if got := libraryReferenceLabel(row); got != "示例景区门票" {
		t.Fatalf("impact label=%q, want visible business name", got)
	}
}

func emptyRecordDatasets() map[string][]model.Record {
	datasets := make(map[string][]model.Record, len(model.Resources()))
	for _, resource := range model.Resources() {
		datasets[string(resource)] = []model.Record{}
	}
	return datasets
}
