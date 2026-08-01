package service

import (
	"testing"

	"my-travel/server/internal/model"
)

func TestResolveTagDependencyUsesIDThenSemanticIdentity(t *testing.T) {
	dependency := model.TripBackupTagDependency{
		SourceID: "source-tag",
		Name:     "景点",
		Category: "place-type",
	}
	rows := []model.LibraryRecord{
		{
			ID:       "source-tag",
			Resource: model.LibraryTags,
			Data: map[string]any{
				"name": "酒店", "category": "place-type",
			},
		},
		{
			ID:       "semantic-tag",
			Resource: model.LibraryTags,
			Data: map[string]any{
				"name": "景点", "category": "place-type",
			},
		},
	}
	if got := resolveTagDependency(dependency, rows); got.Status != "conflict" ||
		got.TargetID != "" {
		t.Fatalf("same ID with different meaning was not treated as conflict: %#v", got)
	}

	rows = rows[1:]
	if got := resolveTagDependency(dependency, rows); got.Status != "matched-semantic" ||
		got.TargetID != "semantic-tag" {
		t.Fatalf("semantic tag was not suggested: %#v", got)
	}
}

func TestImportDependencyRecordKeyIsStableAndScopedToNewTrip(t *testing.T) {
	first := importDependencyRecordKey("member", "checksum", "source", "trip-a")
	replayed := importDependencyRecordKey("member", "checksum", "source", "trip-a")
	secondTrip := importDependencyRecordKey("member", "checksum", "source", "trip-b")
	if first != replayed {
		t.Fatal("same import did not produce a stable record key")
	}
	if first == secondTrip {
		t.Fatal("different imported trips reused the same library record key")
	}
}
