// 日期模型只使用 Day 直接保存的地点顺序。
import type {
  DayData,
  DayPlaceEntry,
  PlaceData,
  RecordRow,
  SegmentData,
  Trip
} from "../../lib/types";
import { currentDateInTimezone } from "../../lib/date";

export { currentDateInTimezone } from "../../lib/date";

export type TripPlanData = {
  days: Array<RecordRow<DayData>>;
  places: Array<RecordRow<PlaceData>>;
  segments: Array<RecordRow<SegmentData>>;
};

export type DayPlanEntry = {
  id: string;
  entry: DayPlaceEntry;
  place?: RecordRow<PlaceData>;
  order: number;
};

export type DayPlan = {
  day: RecordRow<DayData>;
  placesById: Map<string, RecordRow<PlaceData>>;
  entries: DayPlanEntry[];
  segments: Array<RecordRow<SegmentData>>;
};

export type DayWorkspaceMode = "plan" | "travel";

export type AdjacentSegmentGroup = {
  id: string;
  from: DayPlanEntry;
  to: DayPlanEntry;
  segments: Array<RecordRow<SegmentData>>;
};

export type DayReturnTarget = {
  day: RecordRow<DayData>;
  label: "回到今天" | "回到首日";
};

export function ordered<T extends Record<string, unknown>>(rows: Array<RecordRow<T>>): Array<RecordRow<T>> {
  return [...rows].sort((left, right) => (
    left.position - right.position || left.id.localeCompare(right.id)
  ));
}

export function orderedDays(rows: Array<RecordRow<DayData>>): Array<RecordRow<DayData>> {
  return [...rows].sort((left, right) => (
    left.data.date.localeCompare(right.data.date)
    || left.position - right.position
    || left.id.localeCompare(right.id)
  ));
}

export function firstAvailableDate(
  days: Array<RecordRow<DayData>>,
  trip: Pick<Trip, "startDate" | "endDate">
): string | undefined {
  const occupied = new Set(days.map((day) => day.data.date));
  const candidate = new Date(`${trip.startDate}T12:00:00Z`);
  const end = new Date(`${trip.endDate}T12:00:00Z`);
  while (candidate <= end) {
    const value = candidate.toISOString().slice(0, 10);
    if (!occupied.has(value)) return value;
    candidate.setUTCDate(candidate.getUTCDate() + 1);
  }
  return undefined;
}

export function resolveDay(
  days: Array<RecordRow<DayData>>,
  trip: Trip,
  requestedDate: string,
  now = new Date()
): RecordRow<DayData> | undefined {
  const sorted = orderedDays(days).filter((day) => validDate(day.data.date));
  const requested = sorted.find((day) => day.data.date === requestedDate);
  if (requested) return requested;

  const today = currentDateInTimezone(trip.timezone, now);
  if (today >= trip.startDate && today <= trip.endDate) {
    const current = sorted.find((day) => day.data.date === today);
    if (current) return current;
  }
  return sorted[0];
}

export function dayReturnTarget(
  days: Array<RecordRow<DayData>>,
  trip: Trip,
  now = new Date()
): DayReturnTarget | undefined {
  const sorted = orderedDays(days).filter((day) => validDate(day.data.date));
  const today = currentDateInTimezone(trip.timezone, now);
  const todayDay = today >= trip.startDate && today <= trip.endDate
    ? sorted.find((day) => day.data.date === today)
    : undefined;
  if (todayDay) return { day: todayDay, label: "回到今天" };
  return sorted[0] ? { day: sorted[0], label: "回到首日" } : undefined;
}

export function buildDayPlan(data: TripPlanData, day: RecordRow<DayData>): DayPlan {
  const placesById = new Map(data.places.map((place) => [place.id, place]));
  const entries = (day.data.placeEntries ?? []).map((entry, index) => ({
    id: `${day.id}:${index}:${entry.placeId}`,
    entry,
    place: placesById.get(entry.placeId),
    order: index + 1
  }));
  const placeIDs = new Set(entries.map(({ entry }) => entry.placeId));
  return {
    day,
    entries,
    placesById,
    segments: ordered(data.segments.filter((segment) => (
      segment.data.dayId === day.id
      && placeIDs.has(segment.data.fromPlaceId)
      && placeIDs.has(segment.data.toPlaceId)
    )))
  };
}

export function defaultDayWorkspaceMode(
  date: string,
  timezone: string,
  now = new Date()
): DayWorkspaceMode {
  return date === currentDateInTimezone(timezone, now) ? "travel" : "plan";
}

export function selectedPlanEntry(
  plan: DayPlan,
  requestedPlaceID: string
): DayPlanEntry | undefined {
  return plan.entries.find((entry) => entry.entry.placeId === requestedPlaceID)
    ?? plan.entries[0];
}

export function adjacentSegmentGroups(plan: DayPlan): AdjacentSegmentGroup[] {
  return plan.entries.slice(0, -1).map((from, index) => {
    const to = plan.entries[index + 1]!;
    return {
      id: `${from.entry.placeId}:${to.entry.placeId}`,
      from,
      to,
      segments: plan.segments.filter((segment) => (
        segment.data.fromPlaceId === from.entry.placeId
        && segment.data.toPlaceId === to.entry.placeId
      ))
    };
  });
}

export function otherDaySegments(plan: DayPlan): Array<RecordRow<SegmentData>> {
  const adjacentPairs = new Set(
    adjacentSegmentGroups(plan).map((group) => group.id)
  );
  return plan.segments.filter((segment) => !adjacentPairs.has(
    `${segment.data.fromPlaceId}:${segment.data.toPlaceId}`
  ));
}

export function auxiliaryRecordBelongsToDay(
  row: RecordRow,
  day: RecordRow<DayData>
): boolean {
  const dayId = String(row.data.dayId ?? "");
  if (dayId) return dayId === day.id;
  const dayIds = Array.isArray(row.data.dayIds) ? row.data.dayIds.map(String) : [];
  if (dayIds.length) return dayIds.includes(day.id);
  const placeIDs = new Set((day.data.placeEntries ?? []).map((entry) => entry.placeId));
  const placeID = String(row.data.placeId ?? "");
  if (placeID && placeIDs.has(placeID)) return true;
  const date = String(row.data.date ?? row.data.dateTime ?? row.data.occurredAt ?? "");
  return date.slice(0, 10) === day.data.date;
}

export function weatherLocations(plan: DayPlan): Array<{ adcode: string; label: string }> {
  return plan.entries.flatMap(({ place }) => {
    const adcode = String(place?.data.weatherAdcode ?? "");
    return /^\d{6}$/.test(adcode)
      ? [{ adcode, label: String(place?.data.name ?? "路线地点") }]
      : [];
  }).filter((location, index, rows) => (
    rows.findIndex((row) => row.adcode === location.adcode) === index
  ));
}

export function validDate(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(value);
}

export function displayTags(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).map((item) => item.trim()).filter(Boolean);
  return String(value ?? "").split(/[，,、]/).map((item) => item.trim()).filter(Boolean);
}
