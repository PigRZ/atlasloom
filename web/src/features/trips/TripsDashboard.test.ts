import { describe, expect, it } from "vitest";
import { tripCapabilities, tripPhase } from "./TripsDashboard";

describe("trip dashboard phase", () => {
  const trip = { startDate: "2026-09-25", endDate: "2026-09-27" };

  it("derives the simple phase from dates instead of another editable status", () => {
    expect(tripPhase(trip, "2026-09-20")).toBe("待出发");
    expect(tripPhase(trip, "2026-09-26")).toBe("进行中");
    expect(tripPhase(trip, "2026-09-28")).toBe("已结束");
  });
});

describe("trip dashboard permissions", () => {
  it("lets administrators export and delete without granting trip read or edit access", () => {
    expect(tripCapabilities({}, true)).toEqual({
      open: false,
      edit: false,
      manage: true
    });
  });

  it("keeps editor and viewer capabilities scoped to their trip role", () => {
    expect(tripCapabilities({ role: "editor" }, false)).toEqual({
      open: true,
      edit: true,
      manage: false
    });
    expect(tripCapabilities({ role: "viewer" }, false)).toEqual({
      open: true,
      edit: false,
      manage: false
    });
  });
});
