import { describe, expect, it } from "vitest";
import {
  resourceCoreFields,
  resources
} from "../../lib/resourceDefinitions";
import { normalizeRecordData } from "./RecordForm";

function definition(id: string) {
  const result = resources.find((resource) => resource.id === id);
  if (!result) throw new Error(`missing resource definition: ${id}`);
  return result;
}

describe("normalizeRecordData", () => {
  it("keeps each resource core small and valid", () => {
    for (const resource of resources) {
      const core = resourceCoreFields[resource.id];
      const available = new Set(resource.fields.map(([key]) => key));
      expect(core.length).toBeLessThanOrEqual(6);
      expect(core).toContain(resource.fields[0]?.[0]);
      expect(core.every((key) => available.has(key))).toBe(true);
    }
  });

  it("keeps booking identity aligned with its generated expense", () => {
    expect(definition("bookings").fields.map(([key]) => key)).toEqual([
      "item", "type", "bookerMemberId", "placeId", "platformId",
      "dateTime", "status", "link", "notes"
    ]);
    expect(definition("bookings").fields.find(([key]) => key === "dateTime")?.[2])
      .toBe("datetime-local");
  });

  it("keeps research content simple while retaining filter associations", () => {
    expect(definition("research-evidence").fields.map(([key]) => key)).toEqual([
      "title", "content", "platformId", "recordedByMemberId", "dayIds", "placeId"
    ]);
  });

  it("omits empty optional scalar and numeric fields", () => {
    expect(normalizeRecordData(definition("places"), {
      name: "示例东站",
      type: "",
      latitude: "",
      longitude: "",
      navigationURL: ""
    })).toEqual({ name: "示例东站" });
  });

  it("converts the decimal money draft to integer minor units once", () => {
    expect(normalizeRecordData(definition("expenses"), {
      item: "景区门票",
      amountMinor: "12.34"
    })).toMatchObject({
      amountMinor: 1234
    });
  });

  it("normalizes empty multiselects and preserves booleans", () => {
    const normalized = normalizeRecordData(definition("packing-items"), {
      item: "冲锋衣",
      dayIds: "",
      quantity: "",
      packed: false
    });
    expect(normalized).toMatchObject({
      item: "冲锋衣",
      dayIds: [],
      packed: false
    });
    expect(normalized).not.toHaveProperty("quantity");
  });
});
