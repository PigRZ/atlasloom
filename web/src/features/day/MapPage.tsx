// 独立地图页仍以单日为边界，只改变 URL 中的 date，不请求全程路线。
import { ArrowLeft, ArrowRight, CalendarCheck } from "@phosphor-icons/react";
import { useEffect, useMemo } from "react";
import { DataError, PageSkeleton } from "../../components/PageStates";
import { formatDayDate, formatDayTitle } from "../../lib/formatters";
import type { Trip } from "../../lib/types";
import { DeferredRouteMapView } from "../records/DeferredRouteMapView";
import {
  buildDayPlan,
  dayReturnTarget,
  orderedDays,
  resolveDay
} from "./dayModel";
import { useTripPlanData } from "./useTripPlanData";

type MapPageProps = {
  trip: Trip;
  date: string;
  onDateChange: (date: string, replace?: boolean) => void;
};

export function MapPage({ trip, date, onDateChange }: MapPageProps) {
  const planQuery = useTripPlanData(trip);
  const selectedDay = resolveDay(planQuery.data.days, trip, date);
  const days = orderedDays(planQuery.data.days);
  const index = selectedDay ? days.findIndex((day) => day.id === selectedDay.id) : -1;
  const plan = useMemo(
    () => selectedDay ? buildDayPlan(planQuery.data, selectedDay) : undefined,
    [planQuery.data, selectedDay]
  );

  useEffect(() => {
    if (selectedDay && date !== selectedDay.data.date) {
      onDateChange(selectedDay.data.date, true);
    }
  }, [date, onDateChange, selectedDay]);

  if (planQuery.isLoading) return <PageSkeleton />;
  if (planQuery.isError) return <DataError message="路线读取失败" onRetry={planQuery.refetch} />;
  if (!selectedDay || !plan) {
    return <section className="empty-panel"><h1>还没有可查看的日期</h1></section>;
  }
  const returnTarget = dayReturnTarget(days, trip);
  const previous = index > 0 ? days[index - 1] : undefined;
  const next = index >= 0 ? days[index + 1] : undefined;
  return (
    <div className="map-page page-enter">
      <section className="map-day-header">
        <button
          type="button"
          aria-label="前一天"
          disabled={!previous}
          onClick={() => previous && onDateChange(previous.data.date)}
        >
          <ArrowLeft aria-hidden="true" size={18} />
        </button>
        <div>
          <span>D{index + 1} · {formatDayDate(selectedDay.data.date)}</span>
          <h1>{formatDayTitle(selectedDay.data.title)}</h1>
        </div>
        <button
          type="button"
          aria-label="后一天"
          disabled={!next}
          onClick={() => next && onDateChange(next.data.date)}
        >
          <ArrowRight aria-hidden="true" size={18} />
        </button>
        <button
          className="secondary-button"
          type="button"
          disabled={!returnTarget || returnTarget.day.id === selectedDay.id}
          onClick={() => returnTarget && onDateChange(returnTarget.day.data.date)}
        >
          <CalendarCheck aria-hidden="true" size={17} />
          {returnTarget?.label ?? "回到首日"}
        </button>
      </section>
      <DeferredRouteMapView trip={trip} plan={plan} />
    </div>
  );
}
