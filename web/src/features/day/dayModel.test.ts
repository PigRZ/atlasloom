import { describe, expect, it } from "vitest";
import type { DayData, RecordRow, Trip } from "../../lib/types";
import {
  adjacentSegmentGroups,
  auxiliaryRecordBelongsToDay,
  buildDayPlan,
  dayReturnTarget,
  defaultDayWorkspaceMode,
  firstAvailableDate,
  otherDaySegments,
  resolveDay,
  selectedPlanEntry,
  weatherLocations
} from "./dayModel";

function record<T extends Record<string, unknown>>(
  id: string,
  resource: RecordRow["resource"],
  data: T
): RecordRow<T> {
  return {
    id,
    tripId: "trip-a",
    recordKey: id,
    resource,
    position: 0,
    version: 1,
    data,
    createdByMemberId: "member-a",
    updatedByMemberId: "member-a",
    createdAt: "",
    updatedAt: ""
  };
}

const trip: Trip = {
  id: "trip-a",
  title: "旅行",
  summary: "",
  startDate: "2026-09-25",
  endDate: "2026-09-27",
  timezone: "Asia/Shanghai",
  currency: "CNY",
  version: 1
};
const days = [
  record<DayData>("day-a", "days", {
    title: "第一天",
    date: "2026-09-25",
    placeEntries: [{ placeId: "place-a", arrivalTime: "09:00" }]
  }),
  record<DayData>("day-b", "days", { title: "第二天", date: "2026-09-26" })
];

describe("执行日模型", () => {
  it("按请求日期、今天或第一天选择日程", () => {
    expect(resolveDay(days, trip, "2026-09-26")?.id).toBe("day-b");
    expect(resolveDay(days, trip, "", new Date("2026-09-25T03:00:00Z"))?.id).toBe("day-a");
    expect(resolveDay(days, trip, "", new Date("2026-08-01T03:00:00Z"))?.id).toBe("day-a");
  });

  it("行程含今天时返回今天，否则返回首日", () => {
    expect(dayReturnTarget(days, trip, new Date("2026-09-25T03:00:00Z"))).toEqual({
      day: days[0],
      label: "回到今天"
    });
    expect(dayReturnTarget(days, trip, new Date("2026-08-01T03:00:00Z"))).toEqual({
      day: days[0],
      label: "回到首日"
    });
    expect(dayReturnTarget([], trip, new Date("2026-08-01T03:00:00Z"))).toBeUndefined();
  });

  it("找到旅行范围内第一个空日期", () => {
    expect(firstAvailableDate(days, trip)).toBe("2026-09-27");
  });

  it("直接从 Day 的地点顺序构造路线并过滤无关交通段", () => {
    const plan = buildDayPlan({
      days,
      places: [
        record("place-a", "places", { name: "车站", weatherAdcode: "110101" }),
        record("place-b", "places", { name: "酒店" })
      ],
      segments: [
        record("segment-a", "segments", {
          dayId: "day-a",
          fromPlaceId: "place-a",
          toPlaceId: "place-b",
          mode: "drive"
        })
      ]
    }, days[0]!);
    expect(plan.entries.map((entry) => entry.entry.placeId)).toEqual(["place-a"]);
    expect(plan.segments).toEqual([]);
    expect(weatherLocations(plan)).toEqual([{ adcode: "110101", label: "车站" }]);
  });

  it("物品按 dayIds、预订按地点或日期归属执行日", () => {
    expect(auxiliaryRecordBelongsToDay(
      record("item-a", "packing-items", { dayIds: ["day-a"] }),
      days[0]!
    )).toBe(true);
    expect(auxiliaryRecordBelongsToDay(
      record("booking-a", "bookings", { placeId: "place-a" }),
      days[0]!
    )).toBe(true);
    expect(auxiliaryRecordBelongsToDay(
      record("booking-b", "bookings", { dateTime: "2026-09-26 10:00" }),
      days[0]!
    )).toBe(false);
  });

  it("智能默认模式只根据所选日期是否为当天", () => {
    const now = new Date("2026-09-25T03:00:00Z");
    expect(defaultDayWorkspaceMode("2026-09-25", trip.timezone, now)).toBe("travel");
    expect(defaultDayWorkspaceMode("2026-09-26", trip.timezone, now)).toBe("plan");
  });

  it("选中地点失效时回到首个地点", () => {
    const plan = buildDayPlan({
      days,
      places: [record("place-a", "places", { name: "车站" })],
      segments: []
    }, days[0]!);
    expect(selectedPlanEntry(plan, "missing")?.entry.placeId).toBe("place-a");
  });

  it("相邻地点保留多条同向交通，反向和非相邻进入其他参考", () => {
    const day = record<DayData>("day-route", "days", {
      title: "路线日",
      date: "2026-09-25",
      placeEntries: [
        { placeId: "place-a" },
        { placeId: "place-b" },
        { placeId: "place-c" }
      ]
    });
    const plan = buildDayPlan({
      days: [day],
      places: [
        record("place-a", "places", { name: "甲" }),
        record("place-b", "places", { name: "乙" }),
        record("place-c", "places", { name: "丙" })
      ],
      segments: [
        record("segment-drive", "segments", {
          dayId: day.id, fromPlaceId: "place-a", toPlaceId: "place-b", mode: "drive"
        }),
        record("segment-walk", "segments", {
          dayId: day.id, fromPlaceId: "place-a", toPlaceId: "place-b", mode: "walk"
        }),
        record("segment-reverse", "segments", {
          dayId: day.id, fromPlaceId: "place-b", toPlaceId: "place-a", mode: "taxi"
        }),
        record("segment-skip", "segments", {
          dayId: day.id, fromPlaceId: "place-a", toPlaceId: "place-c", mode: "train"
        })
      ]
    }, day);

    expect(adjacentSegmentGroups(plan).map((group) => group.segments.map((row) => row.id)))
      .toEqual([["segment-drive", "segment-walk"], []]);
    expect(otherDaySegments(plan).map((row) => row.id))
      .toEqual(["segment-reverse", "segment-skip"]);
  });
});
