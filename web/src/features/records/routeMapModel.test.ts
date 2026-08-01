import { describe, expect, it } from "vitest";
import type { DayData, PlaceData, RecordRow, SegmentData } from "../../lib/types";
import type { DayPlan } from "../day/dayModel";
import {
  buildRouteDay,
  generatedNavigationURL,
  roadLegs,
  schematicLegs
} from "./routeMapModel";

function row<T extends Record<string, unknown>>(
  id: string,
  resource: RecordRow["resource"],
  data: T
): RecordRow<T> {
  return {
    id, tripId: "trip-a", recordKey: id, resource, position: 0, version: 1,
    data, createdByMemberId: "member-a", updatedByMemberId: "member-a",
    createdAt: "", updatedAt: ""
  };
}
const day = row<DayData>("day-a", "days", {
  title: "出发日",
  date: "2026-09-25",
  placeEntries: [
    { placeId: "place-a" },
    { placeId: "place-b" },
    { placeId: "place-c" },
    { placeId: "place-d" }
  ]
});
const places = [
  row<PlaceData>("place-a", "places", { name: "起点", longitude: 104, latitude: 30 }),
  row<PlaceData>("place-b", "places", { name: "车站", longitude: 103, latitude: 31 }),
  row<PlaceData>("place-c", "places", { name: "终点", longitude: 102, latitude: 32 }),
  row<PlaceData>("place-d", "places", { name: "骑行点", longitude: 101, latitude: 33 })
];
const segments = [
  row<SegmentData>("segment-a", "segments", {
    dayId: day.id, fromPlaceId: "place-a", toPlaceId: "place-b", mode: "drive"
  }),
  row<SegmentData>("segment-b", "segments", {
    dayId: day.id, fromPlaceId: "place-b", toPlaceId: "place-c", mode: "train"
  }),
  row<SegmentData>("segment-c", "segments", {
    dayId: day.id, fromPlaceId: "place-c", toPlaceId: "place-d", mode: "bicycle"
  })
];
const plan: DayPlan = {
  day,
  placesById: new Map(places.map((place) => [place.id, place])),
  entries: day.data.placeEntries!.map((entry, index) => ({
    id: `${day.id}:${index}:${entry.placeId}`,
    entry,
    place: places.find((place) => place.id === entry.placeId),
    order: index + 1
  })),
  segments
};

describe("地图路线模型", () => {
  it("按地点关联交通段并区分道路与示意路线", () => {
    const route = buildRouteDay(plan);
    expect(route.points.map((point) => point.id)).toEqual([
      "place-a",
      "place-b",
      "place-c",
      "place-d"
    ]);
    expect(route.points.map((point) => point.name)).toEqual([
      "起点",
      "车站",
      "终点",
      "骑行点"
    ]);
    expect(roadLegs(route).map((leg) => leg.id)).toEqual(["segment-a", "segment-c"]);
    expect(schematicLegs(route).map((leg) => leg.id)).toEqual(["segment-b"]);
  });

  it("非导航交通方式退化为高德地点标记", () => {
    const point = buildRouteDay(plan).points[1]!;
    expect(generatedNavigationURL(point, "train")).toContain("/marker?");
    expect(generatedNavigationURL(point, "walk")).toContain("mode=walk");
  });
});
