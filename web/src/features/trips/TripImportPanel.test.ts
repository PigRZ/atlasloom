import { describe, expect, it } from "vitest";
import {
  duplicateTripBackupTarget,
  tripBackupMappingRequests
} from "./TripImportPanel";

describe("trip backup dependency mappings", () => {
  it("keeps explicit existing mappings and administrator create choices", () => {
    expect(tripBackupMappingRequests([
      {
        sourceId: "member-source",
        displayName: "来源成员",
        status: "missing"
      },
      {
        sourceId: "member-existing",
        displayName: "已有成员",
        status: "matched-id",
        targetId: "member-local"
      }
    ], {
      "member-source": "__create__",
      "member-existing": "member-local"
    })).toEqual([
      { sourceId: "member-source", targetId: undefined, create: true },
      { sourceId: "member-existing", targetId: "member-local", create: false }
    ]);
  });

  it("rejects merging two source dependencies into one current record", () => {
    expect(duplicateTripBackupTarget({
      first: "member-local",
      second: "member-local"
    })).toBe(true);
    expect(duplicateTripBackupTarget({
      first: "__create__",
      second: "__create__"
    })).toBe(false);
  });
});
