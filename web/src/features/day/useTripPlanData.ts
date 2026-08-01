// 旅行计划查询钩子为日期、日程与地图复用同一组缓存键。
import { useQuery } from "@tanstack/react-query";
import { api } from "../../lib/api";
import type {
  DayData,
  PlaceData,
  RecordRow,
  SegmentData,
  Trip
} from "../../lib/types";
import type { TripPlanData } from "./dayModel";

export function useTripPlanData(trip: Trip) {
  const days = useQuery({
    queryKey: ["records", trip.id, "days"],
    queryFn: () => api.records(trip.id, "days")
  });
  const places = useQuery({
    queryKey: ["records", trip.id, "places"],
    queryFn: () => api.records(trip.id, "places")
  });
  const segments = useQuery({
    queryKey: ["records", trip.id, "segments"],
    queryFn: () => api.records(trip.id, "segments")
  });

  const data: TripPlanData = {
    days: (days.data?.records ?? []) as Array<RecordRow<DayData>>,
    places: (places.data?.records ?? []) as Array<RecordRow<PlaceData>>,
    segments: (segments.data?.records ?? []) as Array<RecordRow<SegmentData>>
  };

  return {
    data,
    isLoading: days.isLoading || places.isLoading || segments.isLoading,
    isError: days.isError || places.isError || segments.isError,
    refetch: () => Promise.all([
      days.refetch(),
      places.refetch(),
      segments.refetch()
    ])
  };
}
