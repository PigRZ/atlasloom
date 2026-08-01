// 日程页以自然日期组织全程，编辑 Day 时不混入地点和交通事实。
import { ArrowRight, CalendarDots, MapPin, Plus, Trash } from "@phosphor-icons/react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { EditorSurface } from "../../components/EditorSurface";
import { useConfirm } from "../../components/ConfirmDialog";
import { DataError, PageSkeleton } from "../../components/PageStates";
import { api } from "../../lib/api";
import { formatDayDate, formatDayTitle } from "../../lib/formatters";
import { resources } from "../../lib/resourceDefinitions";
import type { DayData, RecordRow, SnapshotSummary, Trip } from "../../lib/types";
import { RecordForm } from "../records/RecordForm";
import {
  buildDayPlan,
  currentDateInTimezone,
  firstAvailableDate,
  orderedDays
} from "./dayModel";
import { useTripPlanData } from "./useTripPlanData";

type ItineraryPageProps = {
  trip: Trip;
  onOpenDate: (date: string) => void;
  setError: (message: string) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
};

type Editor =
  | { mode: "create"; idempotencyKey: string }
  | { mode: "edit"; row: RecordRow<DayData> };

export function ItineraryPage({
  trip,
  onOpenDate,
  setError,
  onSnapshot,
  onDirtyChange
}: ItineraryPageProps) {
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const readOnly = trip.role === "viewer";
  const planQuery = useTripPlanData(trip);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [dirty, setDirty] = useState(false);
  const [savedId, setSavedId] = useState("");
  const definition = resources.find((resource) => resource.id === "days")!;
  const closeEditor = useCallback(() => {
    setEditor(null);
    setDirty(false);
    onDirtyChange(false);
  }, [onDirtyChange]);
  const dirtyChanged = useCallback((value: boolean) => {
    setDirty(value);
    onDirtyChange(value);
  }, [onDirtyChange]);
  const cancelEditor = useCallback(async () => {
    if (!dirty || await confirm({
      title: "放弃未保存修改？",
      message: "当前表单有未保存的修改，关闭后不会保留。",
      confirmLabel: "放弃修改",
      tone: "danger"
    })) {
      closeEditor();
    }
  }, [closeEditor, confirm, dirty]);
  const invalidate = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ["records", trip.id] }),
    queryClient.invalidateQueries({ queryKey: ["trips"] })
  ]);
  const create = useMutation({
    mutationFn: ({
      recordKey,
      data,
      idempotencyKey
    }: {
      recordKey: string;
      data: Record<string, unknown>;
      idempotencyKey: string;
    }) => (
      api.createRecord(trip.id, "days", recordKey, data, idempotencyKey)
    ),
    onSuccess: async ({ record, snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      setSavedId(record.id);
      closeEditor();
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : "日程保存失败")
  });
  const update = useMutation({
    mutationFn: ({ row, data }: { row: RecordRow; data: Record<string, unknown> }) => (
      api.updateRecord(trip.id, "days", row, data)
    ),
    onSuccess: async ({ record, snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      setSavedId(record.id);
      closeEditor();
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : "日程保存失败")
  });
  const remove = useMutation({
    mutationFn: (row: RecordRow<DayData>) => api.deleteRecord(trip.id, "days", row),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      setSavedId("");
    },
    onError: (reason) => {
      const message = reason instanceof Error ? reason.message : "执行日删除失败";
      setError(message.toLowerCase().includes("still referenced")
        ? "无法删除：该执行日仍有关联交通段或其他计划，请先处理这些依赖。"
        : message);
    }
  });

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  useEffect(() => {
    if (!savedId) return undefined;
    const timer = window.setTimeout(() => setSavedId(""), 2400);
    return () => window.clearTimeout(timer);
  }, [savedId]);

  if (planQuery.isLoading) return <PageSkeleton />;
  if (planQuery.isError) {
    return <DataError message="日程读取失败" onRetry={planQuery.refetch} />;
  }

  const days = orderedDays(planQuery.data.days);
  const dayPlans = days.map((day) => ({ day, plan: buildDayPlan(planQuery.data, day) }));
  const availableDate = firstAvailableDate(days, trip);
  const placeCount = planQuery.data.places.length;
  const scheduledStopCount = dayPlans.reduce((total, item) => total + item.plan.entries.length, 0);
  const segmentCount = dayPlans.reduce((total, item) => total + item.plan.segments.length, 0);
  const today = currentDateInTimezone(trip.timezone);
  const openCreateEditor = () => {
    if (readOnly || !availableDate) return;
    setEditor(newDayEditor());
  };
  return (
    <div className="itinerary-page page-enter">
      <section className="page-heading">
        <div>
          <h1>日程</h1>
          <p>
            {days.length} 个执行日，{scheduledStopCount} 个地点
            {segmentCount ? `，${segmentCount} 段交通参考` : "，交通按需补充"}
          </p>
        </div>
        {!readOnly && availableDate && (
          <button
            className="primary-button"
            type="button"
            onClick={openCreateEditor}
          >
            <Plus aria-hidden="true" size={17} />补充执行日
          </button>
        )}
      </section>
      {readOnly && (
        <p className="readonly-notice" role="status">
          你拥有本次旅行的只读权限，可以查看和打开日程，但不能调整安排。
        </p>
      )}
      {days.length > 0 && placeCount === 0 && (
        <section className="itinerary-start" aria-labelledby="itinerary-start-title">
          <CalendarDots aria-hidden="true" size={28} weight="duotone" />
          <div>
            <h2 id="itinerary-start-title">日期骨架已准备好</h2>
            <p>已经生成 {days.length} 个执行日。每一天只需先收集想去的地点。</p>
          </div>
          <button
            className="primary-button"
            type="button"
            onClick={() => onOpenDate(days[0]!.data.date)}
          >
            添加第一天地点<ArrowRight aria-hidden="true" size={16} />
          </button>
        </section>
      )}
      {days.length ? (
        <ol className="itinerary-list">
          {dayPlans.map(({ day, plan }, index) => {
            const placePreview = plan.entries
              .map((entry) => String(entry.place?.data.name ?? "未知地点"))
              .slice(0, 3)
              .join("、");
            const remainingStops = Math.max(0, plan.entries.length - 3);
            const isToday = day.data.date === today;
            return (
              <li
                key={day.id}
                className={[
                  savedId === day.id ? "is-highlighted" : "",
                  isToday ? "is-today" : ""
                ].filter(Boolean).join(" ")}
              >
                <button
                  className="itinerary-open"
                  type="button"
                  aria-current={isToday ? "date" : undefined}
                  onClick={() => onOpenDate(day.data.date)}
                >
                  <span className="itinerary-day-index">D{index + 1}</span>
                  <span>
                    <small>
                      {formatDayDate(day.data.date)}
                      {isToday && <i className="itinerary-today-label">今天</i>}
                    </small>
                    <b>{formatDayTitle(day.data.title)}</b>
                    <em>
                      <MapPin aria-hidden="true" size={14} />
                      {plan.entries.length} 个地点
                      {plan.segments.length ? `，${plan.segments.length} 段交通参考` : ""}
                    </em>
                    {placePreview && (
                      <span className="itinerary-stops-preview">
                        {placePreview}{remainingStops ? `，另有 ${remainingStops} 个` : ""}
                      </span>
                    )}
                  </span>
                  <ArrowRight aria-hidden="true" size={19} />
                </button>
                {!readOnly && (
                  <div className="itinerary-actions">
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={() => setEditor({ mode: "edit", row: day })}
                    >
                      编辑
                    </button>
                    <button
                      className="secondary-button danger-button"
                      type="button"
                      disabled={remove.isPending}
                      onClick={async () => {
                        if (await confirm({
                          title: `删除执行日“${day.data.title}”？`,
                          message: "存在地点、交通、预订等关联内容时系统会拒绝删除。",
                          confirmLabel: "确认删除",
                          tone: "danger"
                        })) {
                          remove.mutate(day);
                        }
                      }}
                    >
                      <Trash aria-hidden="true" size={14} />删除
                    </button>
                  </div>
                )}
              </li>
            );
          })}
        </ol>
      ) : (
        <section className="empty-panel">
          <CalendarDots aria-hidden="true" size={32} weight="duotone" />
          <h2>还没有执行日</h2>
          {readOnly
            ? <p>旅行负责人尚未安排执行日。</p>
            : (
              <button
                className="primary-button"
                type="button"
                onClick={openCreateEditor}
              >
                新增执行日
              </button>
            )}
        </section>
      )}
      <p className="sr-only" role="status" aria-live="polite">
        {savedId ? "执行日已保存" : ""}
      </p>
      <EditorSurface
        open={!readOnly && editor !== null}
        title={`${editor?.mode === "edit" ? "编辑" : "新增"}执行日`}
        variant="drawer"
        dirty={dirty}
        onClose={closeEditor}
      >
        {!readOnly && editor && (
          <RecordForm
            definition={definition}
            initial={editor.mode === "edit" ? editor.row.data : undefined}
            defaults={editor.mode === "edit" ? undefined : {
              date: availableDate
            }}
            fieldConstraints={{
              date: {
                min: trip.startDate,
                max: trip.endDate,
                helperText: `旅行日期范围：${trip.startDate} 至 ${trip.endDate}`,
                errorMessage: `执行日必须位于旅行日期范围 ${trip.startDate} 至 ${trip.endDate} 内`
              }
            }}
            pending={create.isPending || update.isPending}
            onDirtyChange={dirtyChanged}
            onCancel={cancelEditor}
            onSubmit={(recordKey, data) => editor.mode === "edit"
              ? update.mutateAsync({ row: editor.row, data }).then(() => undefined)
              : create.mutateAsync({
                recordKey,
                data,
                idempotencyKey: editor.idempotencyKey
              }).then(() => undefined)}
          />
        )}
      </EditorSurface>
    </div>
  );
}

function newDayEditor(): Extract<Editor, { mode: "create" }> {
  return { mode: "create", idempotencyKey: crypto.randomUUID() };
}
