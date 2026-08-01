// 路由测试固定旅行、日期与页面的唯一正式 URL。
import { describe, expect, it } from "vitest";
import {
  appRouteFromPath,
  desktopPrimaryPages,
  mobileMorePages,
  mobilePrimaryPages,
  pagePath,
  pagePathForContext
} from "./navigation";

describe("application routes", () => {
  it("parses a date deep link", () => {
    expect(appRouteFromPath("/travel/app/trips/trip-a/day/2026-09-25")).toEqual({
      page: "today",
      tripId: "trip-a",
      date: "2026-09-25"
    });
  });

  it("parses stable planning pages", () => {
    expect(appRouteFromPath("/app/trips/trip-a/places")).toMatchObject({
      page: "places",
      tripId: "trip-a"
    });
  });

  it("parses the all-trips dashboard", () => {
    expect(appRouteFromPath("/travel/app/trips")).toEqual({
      page: "trips",
      tripId: "",
      date: ""
    });
    expect(pagePath("", "trips")).toBe("/app/trips");
  });

  it("keeps the global library outside every trip", () => {
    expect(appRouteFromPath("/travel/app/library")).toEqual({
      page: "library",
      tripId: "",
      date: ""
    });
    expect(pagePath("", "library")).toBe("/app/library");
    expect(pagePathForContext("trip-a", "library", "2026-09-25"))
      .toBe("/app/library");
  });

  it("falls back to the today entry without inventing a trip", () => {
    expect(appRouteFromPath("/")).toEqual({ page: "today", tripId: "", date: "" });
  });

  it("builds date and regular page paths", () => {
    expect(pagePath("trip-a", "today", "2026-09-25"))
      .toBe("/app/trips/trip-a/day/2026-09-25");
    expect(pagePath("trip-a", "map")).toBe("/app/trips/trip-a/map");
  });

  it("keeps the selected date when moving between pages", () => {
    expect(pagePathForContext("trip-a", "today", "2026-09-25"))
      .toBe("/app/trips/trip-a/day/2026-09-25");
    expect(pagePathForContext("trip-a", "map", "2026-09-25"))
      .toBe("/app/trips/trip-a/map?date=2026-09-25");
    expect(pagePathForContext("trip-a", "more", "2026-09-25"))
      .toBe("/app/trips/trip-a/more?date=2026-09-25");
    expect(pagePathForContext("trip-a", "places", "2026-09-25"))
      .toBe("/app/trips/trip-a/places?date=2026-09-25");
  });

  it("keeps the day map inside today and exposes low-frequency pages through more", () => {
    expect(desktopPrimaryPages.map((page) => page.id)).not.toContain("map");
    expect(desktopPrimaryPages.map((page) => page.id)).not.toContain("more");
    expect(mobilePrimaryPages.map((page) => page.id)).toEqual([
      "today",
      "itinerary",
      "expenses",
      "more"
    ]);
    expect(mobileMorePages.map((page) => page.id)).toEqual([
      "places",
      "bookings",
      "packing-items",
      "members",
      "research-evidence",
      "audit"
    ]);
  });
});
