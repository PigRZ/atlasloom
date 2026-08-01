// 路线投影直接消费执行日中的地点顺序和地点间交通段。
import { transportLabel } from "../../lib/taxonomy";
import type { TransportMode } from "../../lib/types";
import type { DayPlan } from "../day/dayModel";

export type RoutePoint = {
  id: string;
  entry: DayPlan["entries"][number];
  order: number;
  name: string;
  arrivalTime: string;
  departureTime: string;
  additionalInfo: string;
  navigationURL: string;
  longitude: number | null;
  latitude: number | null;
};

export type RouteLeg = {
  id: string;
  row: DayPlan["segments"][number];
  from: RoutePoint;
  to: RoutePoint;
  mode: TransportMode;
  modeLabel: string;
  durationMinutes: number;
  distanceMeters: number;
  roadSupported: boolean;
};

export type RouteDay = {
  id: string;
  title: string;
  date: string;
  additionalInfo: string;
  points: RoutePoint[];
  legs: RouteLeg[];
};

const roadModes = new Set<TransportMode>([
  "walk",
  "drive",
  "taxi",
  "rideshare",
  "bicycle"
]);

export function buildRouteDay(plan: DayPlan): RouteDay {
  const points = plan.entries.map((item): RoutePoint => ({
    id: item.entry.placeId,
    entry: item,
    order: item.order,
    name: item.place?.data.name || "未关联地点",
    arrivalTime: String(item.entry.arrivalTime ?? ""),
    departureTime: String(item.entry.departureTime ?? ""),
    additionalInfo: String(item.entry.additionalInfo ?? ""),
    navigationURL: String(item.place?.data.navigationURL ?? ""),
    longitude: coordinate(item.place?.data.longitude, -180, 180),
    latitude: coordinate(item.place?.data.latitude, -90, 90)
  }));
  const pointByPlace = new Map<string, RoutePoint>();
  points.forEach((point) => {
    if (!pointByPlace.has(point.entry.entry.placeId)) {
      pointByPlace.set(point.entry.entry.placeId, point);
    }
  });
  const legs = plan.segments.flatMap((segment): RouteLeg[] => {
    const from = pointByPlace.get(segment.data.fromPlaceId);
    const to = pointByPlace.get(segment.data.toPlaceId);
    if (!from || !to) return [];
    const mode = segment.data.mode || "other";
    return [{
      id: segment.id,
      row: segment,
      from,
      to,
      mode,
      modeLabel: transportLabel(mode),
      durationMinutes: Number(segment.data.durationMinutes) || 0,
      distanceMeters: Number(segment.data.distanceMeters) || 0,
      roadSupported: roadModes.has(mode)
    }];
  });
  return {
    id: plan.day.id,
    title: plan.day.data.title,
    date: plan.day.data.date,
    additionalInfo: String(plan.day.data.additionalInfo ?? ""),
    points,
    legs
  };
}

export function hasCoordinate(point: RoutePoint): point is RoutePoint & {
  longitude: number;
  latitude: number;
} {
  return point.longitude !== null && point.latitude !== null;
}

export function roadLegs(day: RouteDay): RouteLeg[] {
  return day.legs.filter((leg) => (
    leg.roadSupported && hasCoordinate(leg.from) && hasCoordinate(leg.to)
  ));
}

export function schematicLegs(day: RouteDay): RouteLeg[] {
  return day.legs.filter((leg) => (
    !leg.roadSupported && hasCoordinate(leg.from) && hasCoordinate(leg.to)
  ));
}

export function generatedNavigationURL(point: RoutePoint, mode: TransportMode = "drive"): string {
  if (point.navigationURL) return point.navigationURL;
  if (!hasCoordinate(point)) return "";
  const supportedMode = navigationMode(mode);
  if (!supportedMode) {
    const query = new URLSearchParams({
      position: `${point.longitude},${point.latitude}`,
      name: point.name,
      src: "my-travel",
      coordinate: "gaode",
      callnative: "1"
    });
    return `https://uri.amap.com/marker?${query.toString()}`;
  }
  const query = new URLSearchParams({
    to: `${point.longitude},${point.latitude},${point.name}`,
    mode: supportedMode,
    policy: "1",
    src: "my-travel",
    coordinate: "gaode",
    callnative: "1"
  });
  return `https://uri.amap.com/navigation?${query.toString()}`;
}

export function incomingLeg(day: RouteDay, pointId: string): RouteLeg | undefined {
  return day.legs.find((leg) => leg.to.id === pointId);
}

function navigationMode(mode: TransportMode): string {
  if (["drive", "taxi", "rideshare"].includes(mode)) return "car";
  if (mode === "walk") return "walk";
  if (mode === "bicycle") return "ride";
  if (["public_transit", "shuttle", "scenic_bus"].includes(mode)) return "bus";
  return "";
}

function coordinate(value: unknown, minimum: number, maximum: number): number | null {
  const number = Number(value);
  return Number.isFinite(number) && number >= minimum && number <= maximum
    ? number
    : null;
}
