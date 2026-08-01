import {
  ArrowRight,
  CalendarBlank,
  DownloadSimple,
  FileArrowUp,
  MapPin,
  PencilSimple,
  Plus,
  Trash,
  X
} from "@phosphor-icons/react";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState, type FormEvent } from "react";
import { EditorSurface } from "../../components/EditorSurface";
import { useConfirm } from "../../components/ConfirmDialog";
import { TemporalInput } from "../../components/TemporalInput";
import { api } from "../../lib/api";
import { currentDateInTimezone, inclusiveCalendarDays } from "../../lib/date";
import { formatDateRange } from "../../lib/formatters";
import type {
  CreateTripInput,
  SnapshotSummary,
  Trip
} from "../../lib/types";
import { TripImportPanel } from "./TripImportPanel";

type TripsDashboardProps = {
  trips: Trip[];
  admin: boolean;
  onOpenTrip: (trip: Trip) => void;
  onCreatedTrip: (trip: Trip) => void;
  setError: (message: string) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
};

type Editor = { mode: "create" } | { mode: "edit"; trip: Trip };

export function TripsDashboard({
  trips,
  admin,
  onOpenTrip,
  onCreatedTrip,
  setError,
  onSnapshot,
  onDirtyChange
}: TripsDashboardProps) {
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const [editor, setEditor] = useState<Editor | null>(null);
  const [dirty, setDirty] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [importDirty, setImportDirty] = useState(false);
  const [exportingTripID, setExportingTripID] = useState("");
  const overviews = useQueries({
    queries: trips.map((trip) => ({
      queryKey: ["overview", trip.id],
      queryFn: () => api.overview(trip.id),
      staleTime: 60_000
    }))
  });
  const create = useMutation({
    mutationFn: (input: TripEditorValue) => {
      const payload: CreateTripInput = {
        id: tripSlug(input.startDate),
        title: input.title,
        summary: input.summary,
        startDate: input.startDate,
        endDate: input.endDate,
        timezone: "Asia/Shanghai",
        currency: "CNY"
      };
      return api.createTrip(payload);
    },
    onSuccess: async ({ trip, snapshot }) => {
      onSnapshot(snapshot);
      await queryClient.invalidateQueries({ queryKey: ["trips"] });
      closeEditor();
      onCreatedTrip(trip);
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : "旅行创建失败")
  });
  const update = useMutation({
    mutationFn: ({ trip, value }: { trip: Trip; value: TripEditorValue }) => (
      api.updateTrip(trip, value)
    ),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await queryClient.invalidateQueries({ queryKey: ["trips"] });
      closeEditor();
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : "旅行修改失败")
  });
  const remove = useMutation({
    mutationFn: (trip: Trip) => api.deleteTrip(trip),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await queryClient.invalidateQueries({ queryKey: ["trips"] });
    },
    onError: (reason) => setError(reason instanceof Error ? reason.message : "旅行删除失败")
  });
  const closeEditor = () => {
    setEditor(null);
    setDirty(false);
    onDirtyChange(false);
  };
  const requestClose = async () => {
    if (!dirty || await confirm({
      title: "放弃未保存修改？",
      message: "当前表单有未保存的修改，关闭后不会保留。",
      confirmLabel: "放弃修改",
      tone: "danger"
    })) closeEditor();
  };
  const updateDirty = (value: boolean) => {
    setDirty(value);
    onDirtyChange(value);
  };
  const requestDelete = async (trip: Trip) => {
    if (!await confirm({
      title: `删除“${trip.title}”？`,
      message: "删除后，这次旅行及其日程、地点、费用和快照会从页面隐藏，但数据库中的数据不会被物理删除。",
      confirmLabel: "删除旅行",
      tone: "danger"
    })) return;
    await remove.mutateAsync(trip);
  };
  const closeImport = () => {
    setImportOpen(false);
    setImportDirty(false);
    onDirtyChange(false);
  };
  const requestImportClose = async () => {
    if (!importDirty || await confirm({
      title: "放弃当前导入？",
      message: "已经选择的备份文件和基础资料映射不会保留。",
      confirmLabel: "放弃导入",
      tone: "danger"
    })) closeImport();
  };
  const exportTrip = async (trip: Trip) => {
    setExportingTripID(trip.id);
    setError("");
    try {
      const { blob, filename } = await api.downloadTripBackup(trip.id);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = filename;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "旅行备份导出失败");
    } finally {
      setExportingTripID("");
    }
  };

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const sorted = [...trips].sort((left, right) => right.startDate.localeCompare(left.startDate));
  return (
    <div className="trips-dashboard page-enter">
      <section className="trips-hero">
        <div>
          <h1>{sorted.length ? "旅行总看板" : "创建第一场旅行"}</h1>
          {!sorted.length && (
            <p>填写名称和日期后，系统会生成每一天的日程。成员、预订和费用可以稍后补充。</p>
          )}
        </div>
        <div className="trips-hero-actions">
          {sorted.length ? (
            <>
              <button className="secondary-button" type="button" onClick={() => setImportOpen(true)}>
                <FileArrowUp aria-hidden="true" size={17} weight="bold" />导入旅行
              </button>
              <button className="primary-button" type="button" onClick={() => setEditor({ mode: "create" })}>
                <Plus aria-hidden="true" size={17} weight="bold" />新建旅行
              </button>
            </>
          ) : (
            <>
              <button className="primary-button" type="button" onClick={() => setEditor({ mode: "create" })}>
                <Plus aria-hidden="true" size={17} weight="bold" />创建旅行
              </button>
              <button className="secondary-button" type="button" onClick={() => setImportOpen(true)}>
                <FileArrowUp aria-hidden="true" size={17} weight="bold" />从备份导入
              </button>
            </>
          )}
        </div>
      </section>

      {sorted.length ? (
        <section className="trip-board" aria-label="旅行计划">
          {sorted.map((trip) => {
            const originalIndex = trips.findIndex((item) => item.id === trip.id);
            const counts = overviews[originalIndex]?.data?.counts;
            const phase = tripPhase(trip);
            const capabilities = tripCapabilities(trip, admin);
            return (
              <article className="trip-board-row" key={trip.id}>
                <div className="trip-board-date">
                  <CalendarBlank aria-hidden="true" size={22} weight="duotone" />
                  <span>{phase}</span>
                  <b>{formatDateRange(trip.startDate, trip.endDate)}</b>
                </div>
                <div className="trip-board-main">
                  <h2>{trip.title}</h2>
                  <p>{trip.summary.trim() || "还没有补充旅行说明"}</p>
                  <div className="trip-board-facts">
                    <span><CalendarBlank aria-hidden="true" size={15} />{counts?.days ?? 0} 天日程</span>
                    <span><MapPin aria-hidden="true" size={15} />{counts?.places ?? 0} 个地点</span>
                  </div>
                </div>
                <div className="trip-board-actions">
                  {capabilities.edit && (
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={() => setEditor({ mode: "edit", trip })}
                    >
                      <PencilSimple aria-hidden="true" size={15} />调整
                    </button>
                  )}
                  {capabilities.manage && (
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={exportingTripID === trip.id}
                      onClick={() => exportTrip(trip)}
                    >
                      <DownloadSimple aria-hidden="true" size={15} />
                      {exportingTripID === trip.id ? "导出中…" : "导出"}
                    </button>
                  )}
                  {capabilities.manage && (
                    <button
                      className="secondary-button danger-button"
                      type="button"
                      disabled={remove.isPending}
                      onClick={() => requestDelete(trip)}
                    >
                      <Trash aria-hidden="true" size={15} />删除
                    </button>
                  )}
                  {capabilities.open && (
                    <button className="primary-button" type="button" onClick={() => onOpenTrip(trip)}>
                      打开<ArrowRight aria-hidden="true" size={16} />
                    </button>
                  )}
                </div>
              </article>
            );
          })}
        </section>
      ) : null}

      <EditorSurface
        open={editor !== null}
        title={editor?.mode === "edit" ? "调整旅行" : "新建旅行"}
        variant="modal"
        dirty={dirty}
        onClose={closeEditor}
      >
        {editor && (
          <TripEditor
            trip={editor.mode === "edit" ? editor.trip : undefined}
            pending={create.isPending || update.isPending}
            onDirtyChange={updateDirty}
            onCancel={requestClose}
            onSubmit={(value) => editor.mode === "edit"
              ? update.mutateAsync({ trip: editor.trip, value }).then(() => undefined)
              : create.mutateAsync(value).then(() => undefined)}
          />
        )}
      </EditorSurface>

      <EditorSurface
        open={importOpen}
        title="导入旅行"
        variant="drawer"
        dirty={importDirty}
        onClose={closeImport}
      >
        {importOpen && (
          <TripImportPanel
            admin={admin}
            onImported={(trip) => {
              closeImport();
              onCreatedTrip(trip);
            }}
            onSnapshot={onSnapshot}
            onDirtyChange={(next) => {
              setImportDirty(next);
              onDirtyChange(next);
            }}
            onCancel={requestImportClose}
          />
        )}
      </EditorSurface>
    </div>
  );
}

type TripEditorValue = Pick<Trip, "title" | "summary" | "startDate" | "endDate">;

function TripEditor({
  trip,
  pending,
  onDirtyChange,
  onCancel,
  onSubmit
}: {
  trip?: Trip;
  pending: boolean;
  onDirtyChange: (dirty: boolean) => void;
  onCancel: () => void;
  onSubmit: (value: TripEditorValue) => Promise<void>;
}) {
  const formId = useId().replaceAll(":", "");
  const today = currentDateInTimezone(trip?.timezone ?? "Asia/Shanghai");
  const [initialValue] = useState<TripEditorValue>(() => ({
    title: trip?.title ?? "",
    summary: trip?.summary ?? "",
    startDate: trip?.startDate ?? today,
    endDate: trip?.endDate ?? today
  }));
  const [value, setValue] = useState<TripEditorValue>(initialValue);
  const [error, setError] = useState("");
  const [dirty, setDirty] = useState(false);
  const change = (field: keyof TripEditorValue, next: string) => {
    const nextValue = {
      ...value,
      [field]: next,
      ...(field === "startDate" && value.endDate < next ? { endDate: next } : {})
    };
    const nextDirty = !sameTripEditorValue(nextValue, initialValue);
    setValue(nextValue);
    setDirty(nextDirty);
    onDirtyChange(nextDirty);
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError("");
    if (value.endDate < value.startDate) {
      setError("结束日期不能早于开始日期");
      return;
    }
    if (inclusiveCalendarDays(value.startDate, value.endDate) > 366) {
      setError("旅行日期范围不能超过 366 天");
      return;
    }
    try {
      await onSubmit(value);
      setDirty(false);
      onDirtyChange(false);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存失败，请重试");
    }
  };
  return (
    <form className="record-form trip-editor" onSubmit={submit} aria-busy={pending}>
      <div className="form-title">
        <div><p>{trip ? "编辑" : "新增"}</p><h2>旅行</h2></div>
        <button type="button" onClick={onCancel} aria-label="关闭表单">
          <X aria-hidden="true" size={16} weight="bold" />
        </button>
      </div>
      {dirty && <p className="unsaved-note" role="status">有未保存的修改</p>}
      {!trip && <p className="form-context-note">日期会自动生成对应的执行日，创建后从第一天开始安排地点。</p>}
      <div className="form-fields">
        <label htmlFor={`${formId}-title`}>
          <span>旅行名称<i aria-hidden="true">必填</i></span>
          <input
            id={`${formId}-title`}
            name="title"
            autoComplete="off"
            required
            value={value.title}
            onChange={(event) => change("title", event.target.value)}
          />
        </label>
        <label htmlFor={`${formId}-summary`}>
          <span>旅行说明<small>可选</small></span>
          <textarea
            id={`${formId}-summary`}
            name="summary"
            rows={3}
            value={value.summary}
            placeholder="例如行程重点、同行约定或需要随时留意的事项"
            onChange={(event) => change("summary", event.target.value)}
          />
        </label>
        <div className="record-field temporal-field">
          <label htmlFor={`${formId}-start`}>
            <span>开始日期<i aria-hidden="true">必填</i></span>
          </label>
          <TemporalInput
            id={`${formId}-start`}
            name="startDate"
            autoComplete="off"
            type="date"
            required
            value={value.startDate}
            onChange={(event) => change("startDate", event.target.value)}
          />
        </div>
        <div className="record-field temporal-field">
          <label htmlFor={`${formId}-end`}>
            <span>结束日期<i aria-hidden="true">必填</i></span>
          </label>
          <TemporalInput
            id={`${formId}-end`}
            name="endDate"
            autoComplete="off"
            type="date"
            min={value.startDate}
            required
            value={value.endDate}
            onChange={(event) => change("endDate", event.target.value)}
          />
        </div>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      <div className="form-actions">
        <button className="secondary-button" type="button" onClick={onCancel}>取消</button>
        <button
          className="primary-button"
          type="submit"
          disabled={pending || Boolean(trip && !dirty)}
        >
          {pending
            ? (trip ? "保存中…" : "创建中…")
            : (trip ? "保存旅行" : "创建并安排日程")}
        </button>
      </div>
    </form>
  );
}

function sameTripEditorValue(left: TripEditorValue, right: TripEditorValue): boolean {
  return left.title === right.title
    && left.summary === right.summary
    && left.startDate === right.startDate
    && left.endDate === right.endDate;
}

export function tripPhase(
  trip: Pick<Trip, "startDate" | "endDate"> & Partial<Pick<Trip, "timezone">>,
  today = currentDateInTimezone(trip.timezone ?? "Asia/Shanghai")
): string {
  if (today < trip.startDate) return "待出发";
  if (today > trip.endDate) return "已结束";
  return "进行中";
}

export function tripCapabilities(
  trip: Pick<Trip, "role">,
  admin: boolean
): { open: boolean; edit: boolean; manage: boolean } {
  return {
    open: Boolean(trip.role),
    edit: trip.role === "owner" || trip.role === "editor",
    manage: trip.role === "owner" || admin
  };
}

function tripSlug(startDate: string): string {
  return `trip-${startDate.replaceAll("-", "")}-${crypto.randomUUID().slice(0, 6)}`;
}
