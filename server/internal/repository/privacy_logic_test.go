package repository

import (
	"testing"

	"my-travel/server/internal/model"
)

func TestVisibleAuditLogsForMemberKeepsSharedAndOwnPackingOnly(t *testing.T) {
	rows := []model.AuditLog{
		{ID: "shared", Resource: string(model.Places), RecordLabel: "共享地点"},
		{
			ID: "own-created", Resource: string(model.PackingItems),
			After: map[string]any{"ownerMemberId": "member-a", "item": "自己的物品"},
		},
		{
			ID: "own-deleted", Resource: string(model.PackingItems),
			Before: map[string]any{"ownerMemberId": "member-a", "item": "自己的旧物品"},
		},
		{
			ID: "foreign", Resource: string(model.PackingItems),
			After: map[string]any{"ownerMemberId": "member-b", "item": "他人的物品"},
		},
		{
			ID: "aggregate-reorder", Resource: string(model.PackingItems),
			Before: map[string]any{"ids": []any{"packing-a", "packing-b"}},
		},
	}

	visible := visibleAuditLogsForMember(rows, "member-a")
	if len(visible) != 3 {
		t.Fatalf("visible audit count=%d, want 3: %#v", len(visible), visible)
	}
	for index, id := range []string{"shared", "own-created", "own-deleted"} {
		if visible[index].ID != id {
			t.Fatalf("visible[%d]=%s, want %s", index, visible[index].ID, id)
		}
	}
}
