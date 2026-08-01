// 单日路线视图只消费 DayPlan。
import {
  ArrowSquareOut,
  MapPin,
  Wrench
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { api } from "../../lib/api";
import {
  formatDayTitle,
  formatDistanceMeters,
  formatDurationSeconds
} from "../../lib/formatters";
import type { Trip } from "../../lib/types";
import type { DayPlan } from "../day/dayModel";
import { AmapRouteMap, type RouteMapSummary } from "./AmapRouteMap";
import {
  buildRouteDay,
  generatedNavigationURL,
  incomingLeg,
  type RouteDay,
  type RoutePoint
} from "./routeMapModel";

export type RouteMapViewProps = {
  trip: Trip;
  plan: DayPlan;
};

const emptySummary: RouteMapSummary = {
  distance: 0,
  duration: 0,
  successful: 0,
  total: 0,
  schematic: 0,
  pending: false
};

export function RouteMapView({ trip, plan }: RouteMapViewProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [summary, setSummary] = useState<RouteMapSummary>(emptySummary);
  const mapConfig = useQuery({
    queryKey: ["map-config", trip.id],
    queryFn: () => api.mapConfig(trip.id),
    retry: false
  });
  const day = useMemo(() => buildRouteDay(plan), [plan]);
  const requestedStop = searchParams.get("stop") ?? "";
  const selectedStop = day.points.find((point) => point.id === requestedStop)
    ?? day.points[0];
  const updateParams = useCallback((changes: Record<string, string | null>) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      Object.entries(changes).forEach(([key, value]) => {
        if (value) next.set(key, value);
        else next.delete(key);
      });
      return next;
    }, { replace: true });
  }, [setSearchParams]);
  const selectStop = useCallback((stopId: string) => {
    updateParams({ stop: stopId });
  }, [updateParams]);
  const acceptSummary = useCallback((next: RouteMapSummary) => {
    setSummary(next);
  }, []);

  if (!day.points.length) {
    return (
      <section className="route-experience route-map-empty">
        <MapPin aria-hidden="true" size={30} weight="duotone" />
        <h2>当天还没有地点</h2>
        <p>先添加想去的地点，地图会按保存顺序展示；交通信息可以以后再补。</p>
      </section>
    );
  }

  return (
    <section className="route-experience" aria-label={`${day.date} 当天地点地图`}>
      <header className="route-controls">
        <div>
          <span className="eyebrow">当天地点</span>
          <h2>{formatDayTitle(day.title)}</h2>
          <p>
            {day.date} · {day.points.length} 个地点
            {day.legs.length ? ` · ${day.legs.length} 段交通参考` : " · 顺序仅供参考"}
          </p>
        </div>
        {selectedStop && generatedNavigationURL(
          selectedStop,
          incomingLeg(day, selectedStop.id)?.mode ?? "other"
        ) && (
          <a
            className="route-navigation-link"
            href={generatedNavigationURL(
              selectedStop,
              incomingLeg(day, selectedStop.id)?.mode ?? "other"
            )}
            target="_blank"
            rel="noreferrer"
          >
            在高德打开
            <ArrowSquareOut aria-hidden="true" size={16} />
          </a>
        )}
      </header>

      <div className="route-workspace">
        <div className="route-map-shell">
          {mapConfig.isLoading ? (
            <div className="route-map-loading" role="status">
              <span className="sr-only">正在加载地图配置…</span>
            </div>
          ) : mapConfig.data?.enabled && mapConfig.data.jsapiKey ? (
            <AmapRouteMap
              tripId={trip.id}
              config={mapConfig.data}
              day={day}
              selectedStopId={selectedStop?.id ?? ""}
              onSelectStop={selectStop}
              onSummary={acceptSummary}
            />
          ) : (
            <MapUnavailable
              copy={mapConfig.isError
                ? "地图配置读取失败；地点顺序和外部导航仍可使用。"
                : "地图尚未配置；地点顺序和外部导航仍可使用。"}
              onRetry={mapConfig.isError ? () => mapConfig.refetch() : undefined}
            />
          )}
          <div className="route-map-legend" aria-label="地图图例">
            <span><i className="legend-road" aria-hidden="true" />导航路线</span>
            <span><i className="legend-schematic" aria-hidden="true" />示意连接</span>
            <span><i className="legend-stop" aria-hidden="true" />地点</span>
          </div>
          <div className="route-map-summary">
            <span>{summary.distance ? formatDistanceMeters(summary.distance) : `${day.points.length} 个地点`}</span>
            <span>
              {summary.duration
                ? formatDurationSeconds(summary.duration)
                : summary.pending
                  ? "正在规划路线"
                  : summary.total
                    ? "路线暂不可用"
                    : day.legs.length
                      ? `${summary.schematic || day.legs.filter((leg) => !leg.roadSupported).length} 段示意`
                      : "顺序仅供参考"}
            </span>
          </div>
        </div>

        <aside className="route-side" aria-label="路线地点">
          <RouteStopsPanel day={day} selectedStop={selectedStop} onSelectStop={selectStop} />
        </aside>
      </div>
    </section>
  );
}

function RouteStopsPanel({
  day,
  selectedStop,
  onSelectStop
}: {
  day: RouteDay;
  selectedStop?: RoutePoint;
  onSelectStop: (id: string) => void;
}) {
  return (
    <div className="route-stops-panel">
      <div className="route-side-heading">
        <span>地点清单</span>
        <h2>{day.points.length} 个地点</h2>
      </div>
      <ol className="route-timeline">
        {day.points.map((point) => {
          const leg = incomingLeg(day, point.id);
          return (
            <li key={point.id}>
              {leg && (
                <div className={leg.roadSupported ? "route-leg-label" : "route-leg-label is-schematic"}>
                  {leg.modeLabel}
                  {leg.durationMinutes ? ` · ${leg.durationMinutes} 分钟` : ""}
                </div>
              )}
              <button
                type="button"
                className={[
                  "route-stop-button",
                  point.id === selectedStop?.id ? "is-active" : ""
                ].filter(Boolean).join(" ")}
                aria-pressed={point.id === selectedStop?.id}
                onClick={() => onSelectStop(point.id)}
              >
                <i aria-hidden="true">{point.order}</i>
                <span>
                  <b>{point.name}</b>
                  {point.arrivalTime && <small>约 {point.arrivalTime}</small>}
                </span>
              </button>
            </li>
          );
        })}
      </ol>
      {selectedStop && (
        <div className="selected-stop-note">
          <h3>{selectedStop.name}</h3>
          {selectedStop.additionalInfo && <p>{selectedStop.additionalInfo}</p>}
          {generatedNavigationURL(
            selectedStop,
            incomingLeg(day, selectedStop.id)?.mode ?? "other"
          ) && (
            <a
              href={generatedNavigationURL(
                selectedStop,
                incomingLeg(day, selectedStop.id)?.mode ?? "other"
              )}
              target="_blank"
              rel="noreferrer"
            >
              在高德打开<ArrowSquareOut aria-hidden="true" size={15} />
            </a>
          )}
        </div>
      )}
    </div>
  );
}

function MapUnavailable({
  copy,
  onRetry
}: {
  copy: string;
  onRetry?: () => unknown;
}) {
  return (
    <div className="route-map-unavailable">
      <Wrench aria-hidden="true" size={28} weight="duotone" />
      <b>地图暂不可用</b>
      <p>{copy}</p>
      {onRetry && <button className="secondary-button" type="button" onClick={onRetry}>重试</button>}
    </div>
  );
}
