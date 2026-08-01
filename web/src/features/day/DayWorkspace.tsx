// 旅行沙盘把日期、地点顺序、地图和交通段收拢在同一个空间工作区中。
import {
  ArrowRight,
  CalendarDots,
  MapPin,
  MapTrifold,
  NavigationArrow,
  PencilSimple,
  Plus,
  Trash,
  Wrench
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { api } from "../../lib/api";
import { formatDayDate, formatDayTitle, formatDistanceMeters, formatDurationSeconds } from "../../lib/formatters";
import { transportLabel } from "../../lib/taxonomy";
import type { DayData, RecordRow, SegmentData, Trip } from "../../lib/types";
import { AmapRouteMap, type RouteMapSummary } from "../records/AmapRouteMap";
import { buildRouteDay } from "../records/routeMapModel";
import {
  adjacentSegmentGroups,
  otherDaySegments,
  type DayPlan,
  type DayWorkspaceMode
} from "./dayModel";

type DayWorkspaceProps = {
  trip: Trip;
  days: Array<RecordRow<DayData>>;
  plan: DayPlan;
  selectedPlaceID: string;
  mode: DayWorkspaceMode;
  readOnly: boolean;
  onDateChange: (date: string) => void;
  onModeChange: (mode: DayWorkspaceMode) => void;
  onSelectPlace: (placeID: string) => void;
  onEditDay: () => void;
  onCreateSegment: (fromPlaceID: string, toPlaceID: string) => void;
  onEditSegment: (row: RecordRow<SegmentData>) => void;
  onDeleteSegment: (row: RecordRow<SegmentData>) => void;
};

const emptySummary: RouteMapSummary = {
  distance: 0,
  duration: 0,
  successful: 0,
  total: 0,
  schematic: 0,
  pending: false
};

export function DayWorkspace({
  trip,
  days,
  plan,
  selectedPlaceID,
  mode,
  readOnly,
  onDateChange,
  onModeChange,
  onSelectPlace,
  onEditDay,
  onCreateSegment,
  onEditSegment,
  onDeleteSegment
}: DayWorkspaceProps) {
  const routeDay = useMemo(() => buildRouteDay(plan), [plan]);
  const adjacent = useMemo(() => adjacentSegmentGroups(plan), [plan]);
  const otherSegments = useMemo(() => otherDaySegments(plan), [plan]);
  const [summary, setSummary] = useState<RouteMapSummary>(emptySummary);
  const mapConfig = useQuery({
    queryKey: ["map-config", trip.id],
    queryFn: () => api.mapConfig(trip.id),
    retry: false,
    enabled: routeDay.points.length > 0
  });
  const placeNames = new Map(plan.entries.map((entry) => [
    entry.entry.placeId,
    entry.place?.data.name ?? "未知地点"
  ]));

  return (
    <section className="day-workspace" aria-label="旅行沙盘">
      <DayDateRail
        days={days}
        activeDayID={plan.day.id}
        onDateChange={onDateChange}
      />

      <div className="day-map-column">
        <header className="day-command-bar">
          <div className="day-command-title">
            <span>D{days.findIndex((day) => day.id === plan.day.id) + 1} / {formatDayDate(plan.day.data.date)}</span>
            <h1>{formatDayTitle(plan.day.data.title)}</h1>
            {plan.day.data.additionalInfo && <p>{plan.day.data.additionalInfo}</p>}
          </div>
          <div className="day-command-actions">
            <div className="day-mode-toggle" role="group" aria-label="切换工作模式">
              <button
                type="button"
                className={mode === "plan" ? "is-active" : undefined}
                aria-pressed={mode === "plan"}
                onClick={() => onModeChange("plan")}
              >
                <CalendarDots aria-hidden="true" size={16} />规划
              </button>
              <button
                type="button"
                className={mode === "travel" ? "is-active" : undefined}
                aria-pressed={mode === "travel"}
                onClick={() => onModeChange("travel")}
              >
                <NavigationArrow aria-hidden="true" size={16} />在途
              </button>
            </div>
            {!readOnly && mode === "plan" && (
              <button className="secondary-button day-edit-trigger" type="button" onClick={onEditDay}>
                <PencilSimple aria-hidden="true" size={15} />整理地点
              </button>
            )}
          </div>
        </header>

        <div className="day-map-stage">
          <div className="day-map-visual">
            {routeDay.points.length === 0 ? (
              <MapFallback
                icon={<MapPin aria-hidden="true" size={30} weight="duotone" />}
                title="当天还没有地点"
                copy={readOnly
                  ? "旅行负责人尚未安排当天地点。"
                  : "先收集想去的地点即可，时间和交通可以以后再补。"}
              />
            ) : mapConfig.isLoading ? (
              <div className="day-map-loading" role="status">
                <span className="sr-only">正在读取地图配置…</span>
              </div>
            ) : mapConfig.data?.enabled && mapConfig.data.jsapiKey ? (
              <AmapRouteMap
                tripId={trip.id}
                config={mapConfig.data}
                day={routeDay}
                selectedStopId={selectedPlaceID}
                onSelectStop={onSelectPlace}
                onSummary={setSummary}
              />
            ) : (
              <MapFallback
                icon={<Wrench aria-hidden="true" size={28} weight="duotone" />}
                title="地图暂不可用"
                copy={mapConfig.isError
                  ? "地图配置读取失败，地点顺序和外部导航仍可使用。"
                  : "当前环境未配置高德地图，你仍可维护地点和交通关系。"}
                onRetry={mapConfig.isError ? () => mapConfig.refetch() : undefined}
              />
            )}
            {routeDay.points.length > 0 && (
              <div className="day-map-metrics" aria-label="地图概况">
                <span>{summary.distance ? formatDistanceMeters(summary.distance) : `${routeDay.points.length} 个地点`}</span>
                <span>{summary.duration
                  ? formatDurationSeconds(summary.duration)
                  : summary.pending ? "正在规划路线"
                    : plan.segments.length ? `${plan.segments.length} 段交通参考`
                      : "顺序仅供参考"}</span>
              </div>
            )}
          </div>

          <RouteSequence
            plan={plan}
            mode={mode}
            readOnly={readOnly}
            selectedPlaceID={selectedPlaceID}
            adjacent={adjacent}
            otherSegments={otherSegments}
            placeNames={placeNames}
            onSelectPlace={onSelectPlace}
            onCreateSegment={onCreateSegment}
            onEditSegment={onEditSegment}
            onDeleteSegment={onDeleteSegment}
          />
        </div>
      </div>
    </section>
  );
}

function DayDateRail({
  days,
  activeDayID,
  onDateChange
}: {
  days: Array<RecordRow<DayData>>;
  activeDayID: string;
  onDateChange: (date: string) => void;
}) {
  return (
    <nav className="day-date-rail" aria-label="选择执行日">
      {days.map((day, index) => {
        const active = day.id === activeDayID;
        const [, month = "", date = ""] = day.data.date.split("-");
        return (
          <button
            key={day.id}
            type="button"
            className={active ? "is-active" : undefined}
            aria-current={active ? "date" : undefined}
            aria-label={`第 ${index + 1} 天，${formatDayDate(day.data.date)}，${formatDayTitle(day.data.title)}`}
            title={formatDayTitle(day.data.title)}
            onClick={() => onDateChange(day.data.date)}
          >
            <b>D{index + 1}</b>
            <span>{month}/{date}</span>
          </button>
        );
      })}
    </nav>
  );
}

function RouteSequence({
  plan,
  mode,
  readOnly,
  selectedPlaceID,
  adjacent,
  otherSegments,
  placeNames,
  onSelectPlace,
  onCreateSegment,
  onEditSegment,
  onDeleteSegment
}: {
  plan: DayPlan;
  mode: DayWorkspaceMode;
  readOnly: boolean;
  selectedPlaceID: string;
  adjacent: ReturnType<typeof adjacentSegmentGroups>;
  otherSegments: Array<RecordRow<SegmentData>>;
  placeNames: Map<string, string>;
  onSelectPlace: (placeID: string) => void;
  onCreateSegment: (fromPlaceID: string, toPlaceID: string) => void;
  onEditSegment: (row: RecordRow<SegmentData>) => void;
  onDeleteSegment: (row: RecordRow<SegmentData>) => void;
}) {
  if (!plan.entries.length) return null;

  return (
    <aside className="day-route-sheet" aria-label="当天地点与交通">
      <header>
        <div><MapTrifold aria-hidden="true" size={17} /><b>地点顺序</b></div>
        <span>{plan.entries.length} 个地点</span>
      </header>
      <ol>
        {plan.entries.map((entry, index) => {
          const group = adjacent[index];
          const selected = entry.entry.placeId === selectedPlaceID;
          return (
            <li key={entry.id}>
              <button
                className={`day-route-place${selected ? " is-active" : ""}`}
                type="button"
                aria-pressed={selected}
                onClick={() => onSelectPlace(entry.entry.placeId)}
              >
                <i aria-hidden="true">{entry.order}</i>
                <span>
                  <b>{entry.place?.data.name ?? "地点已删除"}</b>
                  {(entry.entry.arrivalTime || entry.entry.additionalInfo) && (
                    <small>{entry.entry.arrivalTime
                      ? `约 ${entry.entry.arrivalTime}`
                      : entry.entry.additionalInfo}</small>
                  )}
                </span>
              </button>
              {group && (
                <SegmentConnector
                  group={group}
                  editable={mode === "plan" && !readOnly}
                  onCreate={() => onCreateSegment(
                    group.from.entry.placeId,
                    group.to.entry.placeId
                  )}
                  onEdit={onEditSegment}
                  onDelete={onDeleteSegment}
                />
              )}
            </li>
          );
        })}
      </ol>
      {otherSegments.length > 0 && (
        <details className="day-other-segments">
          <summary>其他交通参考（{otherSegments.length}）</summary>
          <div>
            {otherSegments.map((segment) => (
              <article key={segment.id}>
                <span>
                  <b>{placeNames.get(segment.data.fromPlaceId) ?? "未知地点"}</b>
                  <ArrowRight aria-hidden="true" size={13} />
                  <b>{placeNames.get(segment.data.toPlaceId) ?? "未知地点"}</b>
                </span>
                <small>{transportLabel(segment.data.mode)}</small>
                {mode === "plan" && !readOnly && (
                  <div>
                    <button type="button" aria-label="编辑其他交通参考" onClick={() => onEditSegment(segment)}>
                      <PencilSimple aria-hidden="true" size={13} />
                    </button>
                    <button type="button" aria-label="删除其他交通参考" onClick={() => onDeleteSegment(segment)}>
                      <Trash aria-hidden="true" size={13} />
                    </button>
                  </div>
                )}
              </article>
            ))}
          </div>
        </details>
      )}
    </aside>
  );
}

function SegmentConnector({
  group,
  editable,
  onCreate,
  onEdit,
  onDelete
}: {
  group: ReturnType<typeof adjacentSegmentGroups>[number];
  editable: boolean;
  onCreate: () => void;
  onEdit: (row: RecordRow<SegmentData>) => void;
  onDelete: (row: RecordRow<SegmentData>) => void;
}) {
  return (
    <div className="day-route-connector">
      <span className="day-route-line" aria-hidden="true"><ArrowRight size={12} /></span>
      {group.segments.length ? (
        <div className="day-route-segments">
          {group.segments.map((segment) => (
            <div key={segment.id}>
              <button
                type="button"
                disabled={!editable}
                aria-label={editable
                  ? `编辑${group.from.place?.data.name ?? "起点"}到${group.to.place?.data.name ?? "终点"}的${transportLabel(segment.data.mode)}`
                  : undefined}
                onClick={() => editable && onEdit(segment)}
              >
                {transportLabel(segment.data.mode)}
                {segment.data.durationMinutes ? ` ${segment.data.durationMinutes} 分钟` : ""}
              </button>
              {editable && (
                <button
                  type="button"
                  className="day-route-segment-delete"
                  aria-label={`删除${group.from.place?.data.name ?? "起点"}到${group.to.place?.data.name ?? "终点"}的${transportLabel(segment.data.mode)}`}
                  onClick={() => onDelete(segment)}
                >
                  <Trash aria-hidden="true" size={12} />
                </button>
              )}
            </div>
          ))}
          {editable && (
            <button className="day-route-add-more" type="button" onClick={onCreate}>
              <Plus aria-hidden="true" size={12} />再添加
            </button>
          )}
        </div>
      ) : editable ? (
        <button className="day-route-add" type="button" onClick={onCreate}>
          <Plus aria-hidden="true" size={13} />添加交通
        </button>
      ) : (
        <span className="day-route-missing">未添加交通</span>
      )}
    </div>
  );
}

function MapFallback({
  icon,
  title,
  copy,
  onRetry
}: {
  icon: ReactNode;
  title: string;
  copy: string;
  onRetry?: () => unknown;
}) {
  return (
    <div className="day-map-fallback">
      {icon}
      <h2>{title}</h2>
      <p>{copy}</p>
      {onRetry && <button className="secondary-button" type="button" onClick={onRetry}>重试</button>}
    </div>
  );
}
