// 单日控制台直接编辑 Day 与 Place 的关系。
import {
  CalendarCheck,
  Plus
} from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { EditorSurface } from "../../components/EditorSurface";
import { useConfirm } from "../../components/ConfirmDialog";
import { DataError, PageSkeleton } from "../../components/PageStates";
import { TemporalInput } from "../../components/TemporalInput";
import { api } from "../../lib/api";
import { resources } from "../../lib/resourceDefinitions";
import type {
  DayData,
  DayPlaceEntry,
  PlaceData,
  RecordRow,
  SegmentData,
  SnapshotSummary,
  Trip
} from "../../lib/types";
import { generatedNavigationURL, type RoutePoint } from "../records/routeMapModel";
import { PlaceQuickCreate } from "../records/PlaceQuickCreate";
import {
  RecordForm,
  type FieldOption,
  type PlaceCreationRequest
} from "../records/RecordForm";
import {
  auxiliaryRecordBelongsToDay,
  buildDayPlan,
  defaultDayWorkspaceMode,
  orderedDays,
  resolveDay,
  selectedPlanEntry,
  type DayWorkspaceMode
} from "./dayModel";
import { DayWorkspace } from "./DayWorkspace";
import { PlaceContextPanel } from "./PlaceContextPanel";
import { useTripPlanData } from "./useTripPlanData";

type DayConsoleProps = {
  trip: Trip;
  currentMemberId: string;
  date: string;
  onDateChange: (date: string, replace?: boolean) => void;
  onOpenItinerary: () => void;
  setError: (message: string) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
};

type Editor =
  | { kind: "day"; row: RecordRow<DayData> }
  | {
    kind: "segment-create";
    idempotencyKey: string;
    fromPlaceId: string;
    toPlaceId: string;
  }
  | { kind: "segment-edit"; row: RecordRow<SegmentData> }
  | { kind: "booking-create"; idempotencyKey: string; placeId: string }
  | { kind: "booking-edit"; row: RecordRow }
  | { kind: "research-create"; idempotencyKey: string; placeId: string }
  | { kind: "research-edit"; row: RecordRow };

type PlaceCreator = {
  targetLabel: string;
  idempotencyKey: string;
  onCreated: Parameters<PlaceCreationRequest>[1];
  associationDay?: RecordRow<DayData>;
};

export function DayConsole({
  trip,
  currentMemberId,
  date,
  onDateChange,
  onOpenItinerary,
  setError,
  onSnapshot,
  onDirtyChange
}: DayConsoleProps) {
  const readOnly = trip.role === "viewer";
  const [searchParams, setSearchParams] = useSearchParams();
  const planQuery = useTripPlanData(trip);
  const bookings = useQuery({
    queryKey: ["records", trip.id, "bookings"],
    queryFn: () => api.records(trip.id, "bookings")
  });
  const packing = useQuery({
    queryKey: ["records", trip.id, "packing-items", currentMemberId],
    queryFn: () => api.records(trip.id, "packing-items")
  });
  const research = useQuery({
    queryKey: ["records", trip.id, "research-evidence"],
    queryFn: () => api.records(trip.id, "research-evidence")
  });
  const tripMembers = useQuery({
    queryKey: ["records", trip.id, "members"],
    queryFn: () => api.records(trip.id, "members")
  });
  const globalMembers = useQuery({
    queryKey: ["library", "members"],
    queryFn: () => api.libraryRecords("members")
  });
  const tags = useQuery({
    queryKey: ["library", "tags"],
    queryFn: () => api.libraryRecords("tags")
  });
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const [editor, setEditor] = useState<Editor | null>(null);
  const [dirty, setDirty] = useState(false);
  const [placeFormDirty, setPlaceFormDirty] = useState(false);
  const [placeCreator, setPlaceCreator] = useState<PlaceCreator | null>(null);

  const days = orderedDays(planQuery.data.days);
  const selectedDay = resolveDay(days, trip, date);
  const plan = selectedDay ? buildDayPlan(planQuery.data, selectedDay) : undefined;
  const explicitMode = searchParams.get("mode");
  const mode: DayWorkspaceMode = explicitMode === "plan" || explicitMode === "travel"
    ? explicitMode
    : defaultDayWorkspaceMode(selectedDay?.data.date ?? "", trip.timezone);
  const requestedPlaceID = searchParams.get("place") ?? "";
  const selectedEntry = plan ? selectedPlanEntry(plan, requestedPlaceID) : undefined;

  useEffect(() => {
    if (selectedDay && date !== selectedDay.data.date) {
      onDateChange(selectedDay.data.date, true);
    }
  }, [date, onDateChange, selectedDay]);
  useEffect(() => {
    if (!plan) return;
    const resolvedPlaceID = selectedEntry?.entry.placeId ?? "";
    if (requestedPlaceID === resolvedPlaceID) return;
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (resolvedPlaceID) next.set("place", resolvedPlaceID);
      else next.delete("place");
      return next;
    }, { replace: true });
  }, [plan, requestedPlaceID, selectedEntry?.entry.placeId, setSearchParams]);
  useEffect(() => {
    onDirtyChange(dirty || placeFormDirty);
  }, [dirty, onDirtyChange, placeFormDirty]);
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  const invalidate = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ["records", trip.id] }),
    queryClient.invalidateQueries({ queryKey: ["audit-logs", trip.id] }),
    queryClient.invalidateQueries({ queryKey: ["trips"] })
  ]);
  const updateDay = useMutation({
    mutationFn: ({ row, data }: { row: RecordRow<DayData>; data: DayData }) => (
      api.updateRecord(trip.id, "days", row, data)
    ),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "日程保存失败"))
  });
  const createSegment = useMutation({
    mutationFn: ({
      data,
      idempotencyKey
    }: {
      data: Record<string, unknown>;
      idempotencyKey: string;
    }) => api.createRecord(trip.id, "segments", crypto.randomUUID(), data, idempotencyKey),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "交通段保存失败"))
  });
  const updateSegment = useMutation({
    mutationFn: ({ row, data }: { row: RecordRow<SegmentData>; data: Record<string, unknown> }) => (
      api.updateRecord(trip.id, "segments", row, data)
    ),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "交通段保存失败"))
  });
  const deleteSegment = useMutation({
    mutationFn: (row: RecordRow<SegmentData>) => api.deleteRecord(trip.id, "segments", row),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
    },
    onError: (reason) => setError(errorText(reason, "交通段删除失败"))
  });
  const createContextRecord = useMutation({
    mutationFn: ({
      resource,
      data,
      recordKey,
      idempotencyKey
    }: {
      resource: "bookings" | "research-evidence";
      data: Record<string, unknown>;
      recordKey: string;
      idempotencyKey: string;
    }) => api.createRecord(trip.id, resource, recordKey, data, idempotencyKey),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "关联资料保存失败"))
  });
  const updateContextRecord = useMutation({
    mutationFn: ({
      resource,
      row,
      data
    }: {
      resource: "bookings" | "research-evidence";
      row: RecordRow;
      data: Record<string, unknown>;
    }) => api.updateRecord(trip.id, resource, row, data),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "关联资料保存失败"))
  });

  function closeEditor() {
    setEditor(null);
    setPlaceCreator(null);
    setDirty(false);
    setPlaceFormDirty(false);
  }
  async function requestClose() {
    if (!dirty || await confirm({
      title: "放弃未保存修改？",
      message: "当前表单有未保存的修改。",
      confirmLabel: "放弃修改",
      tone: "danger"
    })) closeEditor();
  }
  function dirtyChanged(value: boolean) {
    setDirty(value);
  }
  function requestPlaceCreation(
    targetLabel: string,
    onCreated: Parameters<PlaceCreationRequest>[1],
    associationDay?: RecordRow<DayData>
  ) {
    setPlaceCreator({
      targetLabel,
      onCreated,
      associationDay,
      idempotencyKey: crypto.randomUUID()
    });
  }
  function closePlaceCreator() {
    setPlaceCreator(null);
    setPlaceFormDirty(false);
  }

  if (planQuery.isLoading || bookings.isLoading || packing.isLoading || research.isLoading) {
    return <PageSkeleton />;
  }
  if (planQuery.isError || bookings.isError || packing.isError || research.isError) {
    return (
      <DataError
        message="当日日程读取失败"
        onRetry={() => Promise.all([
          planQuery.refetch(),
          bookings.refetch(),
          packing.refetch(),
          research.refetch()
        ])}
      />
    );
  }
  if (!selectedDay || !plan) {
    return (
      <section className="empty-panel">
        <CalendarCheck aria-hidden="true" size={32} weight="duotone" />
        <h1>还没有可执行的日程</h1>
        <p>先在日程页补充一个执行日，再回到今天查看当天计划。</p>
        <button className="primary-button" type="button" onClick={onOpenItinerary}>
          前往日程
        </button>
      </section>
    );
  }

  const dayPacking = (packing.data?.records ?? []).filter((row) => auxiliaryRecordBelongsToDay(row, selectedDay));
  const selectedPlaceID = selectedEntry?.entry.placeId ?? "";
  const selectedBookings = (bookings.data?.records ?? []).filter((row) => (
    String(row.data.placeId ?? "") === selectedPlaceID
  ));
  const selectedResearch = (research.data?.records ?? []).filter((row) => (
    String(row.data.placeId ?? "") === selectedPlaceID
  ));
  const segmentDefinition = resources.find((resource) => resource.id === "segments")!;
  const bookingDefinition = resources.find((resource) => resource.id === "bookings")!;
  const researchDefinition = resources.find((resource) => resource.id === "research-evidence")!;
  const activeTags = (tags.data?.records ?? []).filter(
    (tag) => String(tag.data.status ?? "active") === "active"
  );
  const placeTypeOptions = activeTags
    .filter((tag) => String(tag.data.category ?? "") === "place-type")
    .map((tag) => ({
      value: tag.id,
      label: String(tag.data.name ?? tag.recordKey)
    }));
  const expenseTypeOptions = activeTags
    .filter((tag) => String(tag.data.category ?? "") === "expense-type")
    .map((tag) => ({ value: tag.id, label: String(tag.data.name ?? tag.recordKey) }));
  const platformTypeOptions = activeTags
    .filter((tag) => String(tag.data.category ?? "") === "platform-type")
    .map((tag) => ({ value: tag.id, label: String(tag.data.name ?? tag.recordKey) }));
  const placeTypeUnavailable = tags.isSuccess && placeTypeOptions.length === 0;
  const segmentPlaceOptions = plan.entries.map(({ entry, place }) => ({
    value: entry.placeId,
    label: String(place?.data.name ?? "未知地点")
  }));
  const allPlaceOptions = planQuery.data.places.map((place) => ({
    value: place.id,
    label: String(place.data.name ?? place.recordKey)
  }));
  const globalMemberNames = new Map((globalMembers.data?.records ?? []).map((member) => [
    member.id,
    String(member.data.displayName ?? member.recordKey)
  ]));
  const memberOptions = (tripMembers.data?.records ?? []).map((member) => {
    const memberID = String(member.data.memberId ?? member.id);
    return {
      value: memberID,
      label: globalMemberNames.get(memberID) ?? member.recordKey
    };
  });
  const selectedMemberIDs = new Set(memberOptions.map((member) => member.value));
  const defaultMemberID = selectedMemberIDs.has(currentMemberId)
    ? currentMemberId
    : memberOptions[0]?.value ?? "";
  const dayOptions = days.map((day) => ({
    value: day.id,
    label: `${day.data.date} · ${day.data.title}`
  }));
  const createBookingReady = tags.isSuccess
    && tripMembers.isSuccess
    && expenseTypeOptions.length > 0
    && memberOptions.length > 0;
  const createResearchReady = tags.isSuccess
    && tripMembers.isSuccess
    && platformTypeOptions.length > 0
    && memberOptions.length > 0;
  const creationPlaceDisabled = placeTypeUnavailable || tags.isLoading || tags.isError;
  const creationPlaceDisabledReason = tags.isError
    ? "地点类型读取失败，请刷新后重试"
    : placeTypeUnavailable
      ? "请先在基础资料中维护地点类型"
      : tags.isLoading ? "正在读取地点类型" : "";
  const selectedIncoming = selectedEntry
    ? plan.segments.find((segment) => segment.data.toPlaceId === selectedEntry.entry.placeId)
    : undefined;
  const navigationURL = selectedEntry
    ? navigationForEntry(selectedEntry.place, selectedEntry.entry, selectedIncoming)
    : "";

  function updateWorkspaceParam(key: "mode" | "place", value: string) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (value) next.set(key, value);
      else next.delete(key);
      return next;
    }, { replace: true });
  }

  async function requestSegmentDelete(row: RecordRow<SegmentData>) {
    if (await confirm({
      title: "删除这段交通？",
      message: "地点顺序会保留，只删除这条交通参考。",
      confirmLabel: "确认删除",
      tone: "danger"
    })) deleteSegment.mutate(row);
  }

  return (
    <div className="day-console page-enter">
      {readOnly && (
        <p className="readonly-notice day-readonly-notice" role="status">
          当前为只读访问，导航、天气和关联资料仍可正常查看。
        </p>
      )}
      <div className="day-sandbox">
        <DayWorkspace
          trip={trip}
          days={days}
          plan={plan}
          selectedPlaceID={selectedPlaceID}
          mode={mode}
          readOnly={readOnly}
          onDateChange={onDateChange}
          onModeChange={(nextMode) => updateWorkspaceParam("mode", nextMode)}
          onSelectPlace={(placeID) => updateWorkspaceParam("place", placeID)}
          onEditDay={() => setEditor({ kind: "day", row: selectedDay })}
          onCreateSegment={(fromPlaceId, toPlaceId) => setEditor({
            kind: "segment-create",
            fromPlaceId,
            toPlaceId,
            idempotencyKey: crypto.randomUUID()
          })}
          onEditSegment={(row) => setEditor({ kind: "segment-edit", row })}
          onDeleteSegment={(row) => void requestSegmentDelete(row)}
        />
        <PlaceContextPanel
          mode={mode}
          trip={trip}
          day={selectedDay}
          entry={selectedEntry}
          bookings={selectedBookings}
          research={selectedResearch}
          packing={dayPacking}
          navigationURL={navigationURL}
          readOnly={readOnly}
          canCreateBooking={createBookingReady}
          canCreateResearch={createResearchReady}
          bookingDisabledReason={bookingCreationReason(
            tags.isError || tripMembers.isError,
            expenseTypeOptions.length,
            memberOptions.length
          )}
          researchDisabledReason={researchCreationReason(
            tags.isError || tripMembers.isError,
            platformTypeOptions.length,
            memberOptions.length
          )}
          onCreateBooking={() => selectedPlaceID && setEditor({
            kind: "booking-create",
            placeId: selectedPlaceID,
            idempotencyKey: crypto.randomUUID()
          })}
          onEditBooking={(row) => setEditor({ kind: "booking-edit", row })}
          onCreateResearch={() => selectedPlaceID && setEditor({
            kind: "research-create",
            placeId: selectedPlaceID,
            idempotencyKey: crypto.randomUUID()
          })}
          onEditResearch={(row) => setEditor({ kind: "research-edit", row })}
        />
      </div>

      <EditorSurface
        open={!readOnly && editor !== null}
        title={editorTitle(editor)}
        variant="drawer"
        dirty={dirty}
        onClose={closeEditor}
      >
        {!readOnly && editor?.kind === "day" && (
          <DayEditor
            row={editor.row}
            places={planQuery.data.places}
            startDate={trip.startDate}
            endDate={trip.endDate}
            pending={updateDay.isPending}
            createPlaceDisabled={placeTypeUnavailable || tags.isLoading || tags.isError}
            createPlaceDisabledReason={
              tags.isError
                ? "地点类型读取失败，请刷新后重试"
                : placeTypeUnavailable
                  ? "请先在基础资料中维护地点类型"
                  : tags.isLoading ? "正在读取地点类型" : ""
            }
            onCreatePlace={(targetLabel, onCreated) => requestPlaceCreation(
              targetLabel,
              onCreated
            )}
            onDirtyChange={dirtyChanged}
            onCancel={requestClose}
            onSubmit={(data) => updateDay.mutateAsync({ row: editor.row, data }).then(() => undefined)}
          />
        )}
        {!readOnly && (editor?.kind === "segment-create" || editor?.kind === "segment-edit") && (
          <RecordForm
            definition={segmentDefinition}
            initial={editor.kind === "segment-edit" ? editor.row.data : undefined}
            defaults={editor.kind === "segment-create" ? {
              dayId: selectedDay.id,
              fromPlaceId: editor.fromPlaceId,
              toPlaceId: editor.toPlaceId,
              mode: ""
            } : undefined}
            options={{
              dayId: [{ value: selectedDay.id, label: `${selectedDay.data.date} · ${selectedDay.data.title}` }],
              fromPlaceId: segmentPlaceOptions,
              toPlaceId: segmentPlaceOptions
            }}
            onCreatePlace={(targetLabel, onCreated) => requestPlaceCreation(
              targetLabel,
              onCreated,
              selectedDay
            )}
            createPlaceDisabled={placeTypeUnavailable || tags.isLoading || tags.isError}
            createPlaceDisabledReason={
              tags.isError
                ? "地点类型读取失败，请刷新后重试"
                : placeTypeUnavailable
                  ? "请先在基础资料中维护地点类型"
                  : tags.isLoading ? "正在读取地点类型" : ""
            }
            pending={createSegment.isPending || updateSegment.isPending}
            onDirtyChange={dirtyChanged}
            onCancel={requestClose}
            onSubmit={(_recordKey, data) => editor.kind === "segment-edit"
              ? updateSegment.mutateAsync({ row: editor.row, data }).then(() => undefined)
              : createSegment.mutateAsync({ data, idempotencyKey: editor.idempotencyKey }).then(() => undefined)}
          />
        )}
        {!readOnly && (editor?.kind === "booking-create" || editor?.kind === "booking-edit") && (
          <RecordForm
            definition={bookingDefinition}
            initial={editor.kind === "booking-edit" ? editor.row.data : undefined}
            defaults={editor.kind === "booking-create" ? {
              placeId: editor.placeId,
              bookerMemberId: defaultMemberID,
              status: "needs_booking",
              dateTime: ""
            } : undefined}
            options={{
              placeId: allPlaceOptions,
              bookerMemberId: memberOptions,
              type: expenseTypeOptions,
              platformId: platformTypeOptions
            }}
            onCreatePlace={(targetLabel, onCreated) => requestPlaceCreation(
              targetLabel,
              onCreated
            )}
            createPlaceDisabled={creationPlaceDisabled}
            createPlaceDisabledReason={creationPlaceDisabledReason}
            pending={createContextRecord.isPending || updateContextRecord.isPending}
            onDirtyChange={dirtyChanged}
            onCancel={requestClose}
            onSubmit={(recordKey, data) => editor.kind === "booking-edit"
              ? updateContextRecord.mutateAsync({
                resource: "bookings",
                row: editor.row,
                data
              }).then(() => undefined)
              : createContextRecord.mutateAsync({
                resource: "bookings",
                recordKey,
                data,
                idempotencyKey: editor.idempotencyKey
              }).then(() => undefined)}
          />
        )}
        {!readOnly && (editor?.kind === "research-create" || editor?.kind === "research-edit") && (
          <RecordForm
            definition={researchDefinition}
            initial={editor.kind === "research-edit" ? editor.row.data : undefined}
            defaults={editor.kind === "research-create" ? {
              placeId: editor.placeId,
              dayIds: [selectedDay.id],
              recordedByMemberId: defaultMemberID
            } : undefined}
            options={{
              placeId: allPlaceOptions,
              dayIds: dayOptions,
              recordedByMemberId: memberOptions,
              platformId: platformTypeOptions
            }}
            onCreatePlace={(targetLabel, onCreated) => requestPlaceCreation(
              targetLabel,
              onCreated
            )}
            createPlaceDisabled={creationPlaceDisabled}
            createPlaceDisabledReason={creationPlaceDisabledReason}
            pending={createContextRecord.isPending || updateContextRecord.isPending}
            onDirtyChange={dirtyChanged}
            onCancel={requestClose}
            onSubmit={(recordKey, data) => editor.kind === "research-edit"
              ? updateContextRecord.mutateAsync({
                resource: "research-evidence",
                row: editor.row,
                data
              }).then(() => undefined)
              : createContextRecord.mutateAsync({
                resource: "research-evidence",
                recordKey,
                data,
                idempotencyKey: editor.idempotencyKey
              }).then(() => undefined)}
          />
        )}
      </EditorSurface>
      {!readOnly && placeCreator && (
        <PlaceQuickCreate
          tripId={trip.id}
          targetLabel={placeCreator.targetLabel}
          idempotencyKey={placeCreator.idempotencyKey}
          placeTypeOptions={placeTypeOptions}
          associationDay={placeCreator.associationDay}
          onSnapshot={onSnapshot}
          onCreated={placeCreator.onCreated}
          onDirtyChange={setPlaceFormDirty}
          onClose={closePlaceCreator}
        />
      )}
    </div>
  );
}

function DayEditor({
  row,
  places,
  startDate,
  endDate,
  pending,
  createPlaceDisabled,
  createPlaceDisabledReason,
  onCreatePlace,
  onDirtyChange,
  onCancel,
  onSubmit
}: {
  row: RecordRow<DayData>;
  places: Array<RecordRow<PlaceData>>;
  startDate: string;
  endDate: string;
  pending: boolean;
  createPlaceDisabled: boolean;
  createPlaceDisabledReason: string;
  onCreatePlace: PlaceCreationRequest;
  onDirtyChange: (dirty: boolean) => void;
  onCancel: () => void;
  onSubmit: (data: DayData) => Promise<void>;
}) {
  const [title, setTitle] = useState(row.data.title);
  const [date, setDate] = useState(row.data.date);
  const [additionalInfo, setAdditionalInfo] = useState(String(row.data.additionalInfo ?? ""));
  const [entries, setEntries] = useState<DayPlaceEntry[]>(row.data.placeEntries ?? []);
  const [reorderMessage, setReorderMessage] = useState("");
  const reorderFocusTargetRef = useRef("");
  const reorderButtonRefs = useRef(new Map<string, HTMLButtonElement>());
  const [createdPlaceOptions, setCreatedPlaceOptions] = useState<FieldOption[]>([]);
  const [saving, setSaving] = useState(false);
  const placeOptions = mergePlaceOptions(
    places.map((place) => ({ value: place.id, label: place.data.name })),
    createdPlaceOptions
  );
  const names = new Map(placeOptions.map((place) => [place.value, place.label]));
  const used = new Set(entries.map((entry) => entry.placeId));
  const dirty = title !== row.data.title
    || date !== row.data.date
    || additionalInfo !== String(row.data.additionalInfo ?? "")
    || JSON.stringify(entries) !== JSON.stringify(row.data.placeEntries ?? []);
  const dateOutOfRange = date < startDate || date > endDate;

  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange]);
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  useEffect(() => {
    const target = reorderFocusTargetRef.current;
    if (!target) return;
    reorderFocusTargetRef.current = "";
    reorderButtonRefs.current.get(target)?.focus();
  }, [entries]);

  return (
    <form
      className="record-form day-editor"
      onSubmit={async (event) => {
        event.preventDefault();
        if (dateOutOfRange) return;
        setSaving(true);
        try {
          await onSubmit({ title, date, additionalInfo, placeEntries: entries });
        } finally {
          setSaving(false);
        }
      }}
    >
      <details className="day-editor-metadata">
        <summary>日期与当天说明</summary>
        <div className="form-fields">
          <label>
            <span>标题<i>必填</i></span>
            <input
              name="title"
              autoComplete="off"
              required
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
          </label>
          <div className="record-field temporal-field">
            <label htmlFor="day-editor-date"><span>日期<i>必填</i></span></label>
            <TemporalInput
              id="day-editor-date"
              name="date"
              required
              type="date"
              min={startDate}
              max={endDate}
              value={date}
              aria-describedby="day-editor-date-range"
              onChange={(event) => setDate(event.target.value)}
            />
            <small id="day-editor-date-range" className="field-constraint-note">
              旅行日期范围：{startDate} 至 {endDate}
            </small>
          </div>
          {dateOutOfRange && (
            <p className="form-error day-range-error" role="alert">
              执行日必须位于旅行日期范围 {startDate} 至 {endDate} 内
            </p>
          )}
          <label>
            <span>补充信息</span>
            <textarea
              name="additionalInfo"
              rows={3}
              value={additionalInfo}
              onChange={(event) => setAdditionalInfo(event.target.value)}
            />
          </label>
        </div>
      </details>
      <section className="day-entry-editor">
        <div className="day-entry-heading">
          <h3>想去的地点</h3>
          <p>只需选择地点并调整大致顺序。地点备注和时间都可以不填。</p>
        </div>
        <p className="sr-only" role="status" aria-live="polite">{reorderMessage}</p>
        {entries.map((entry, index) => (
          <article key={entry.placeId}>
            <div>
              <b>{names.get(entry.placeId) ?? "未知地点"}</b>
              <button
                type="button"
                disabled={index === 0}
                aria-label={`将${names.get(entry.placeId) ?? "地点"}上移`}
                ref={(node) => {
                  const key = `${entry.placeId}:up`;
                  if (node) reorderButtonRefs.current.set(key, node);
                  else reorderButtonRefs.current.delete(key);
                }}
                onClick={() => {
                  const newIndex = index - 1;
                  setEntries(moveEntry(entries, index, -1));
                  reorderFocusTargetRef.current =
                    `${entry.placeId}:${newIndex === 0 ? "down" : "up"}`;
                  setReorderMessage(
                    `${names.get(entry.placeId) ?? "地点"}已移到第 ${index} 位`
                  );
                }}
              >上移</button>
              <button
                type="button"
                disabled={index === entries.length - 1}
                aria-label={`将${names.get(entry.placeId) ?? "地点"}下移`}
                ref={(node) => {
                  const key = `${entry.placeId}:down`;
                  if (node) reorderButtonRefs.current.set(key, node);
                  else reorderButtonRefs.current.delete(key);
                }}
                onClick={() => {
                  const newIndex = index + 1;
                  setEntries(moveEntry(entries, index, 1));
                  reorderFocusTargetRef.current =
                    `${entry.placeId}:${newIndex === entries.length - 1 ? "up" : "down"}`;
                  setReorderMessage(
                    `${names.get(entry.placeId) ?? "地点"}已移到第 ${index + 2} 位`
                  );
                }}
              >下移</button>
              <button
                className="danger"
                type="button"
                aria-label={`从当天删除${names.get(entry.placeId) ?? "地点"}`}
                onClick={() => setEntries(entries.filter((_, itemIndex) => itemIndex !== index))}
              >
                删除
              </button>
            </div>
            <div className="day-entry-fields">
              <label className="day-entry-note">
                <span>地点备注（可选）</span>
                <input
                  name={`placeEntries[${index}].additionalInfo`}
                  autoComplete="off"
                  value={entry.additionalInfo ?? ""}
                  onChange={(event) => setEntries(updateEntry(
                    entries,
                    index,
                    "additionalInfo",
                    event.target.value
                  ))}
                />
              </label>
              <details className="day-entry-time">
                <summary>
                  {entry.arrivalTime || entry.departureTime
                    ? `时间参考：${[entry.arrivalTime, entry.departureTime].filter(Boolean).join(" 至 ")}`
                    : "补充时间（可选）"}
                </summary>
                <div className="day-entry-time-fields">
                  <div className="record-field temporal-field">
                    <label htmlFor={`arrival-${index}`}><span>大约到达</span></label>
                    <TemporalInput
                      id={`arrival-${index}`}
                      name={`placeEntries[${index}].arrivalTime`}
                      type="time"
                      value={entry.arrivalTime ?? ""}
                      pickerLabel={`选择${names.get(entry.placeId) ?? "地点"}的大约到达时间`}
                      onChange={(event) => setEntries(updateEntry(entries, index, "arrivalTime", event.target.value))}
                    />
                  </div>
                  <div className="record-field temporal-field">
                    <label htmlFor={`departure-${index}`}><span>大约离开</span></label>
                    <TemporalInput
                      id={`departure-${index}`}
                      name={`placeEntries[${index}].departureTime`}
                      type="time"
                      value={entry.departureTime ?? ""}
                      pickerLabel={`选择${names.get(entry.placeId) ?? "地点"}的大约离开时间`}
                      onChange={(event) => setEntries(updateEntry(entries, index, "departureTime", event.target.value))}
                    />
                  </div>
                </div>
              </details>
            </div>
          </article>
        ))}
        <div className="record-field place-association-field">
          <label htmlFor="day-editor-place"><span>添加地点</span></label>
          <div className="place-association-control">
            <select
              id="day-editor-place"
              name="placeToAdd"
              value=""
              onChange={(event) => {
                if (event.target.value) {
                  setEntries([...entries, { placeId: event.target.value }]);
                }
              }}
            >
              <option value="">
                {placeOptions.some((place) => !used.has(place.value))
                  ? "选择地点"
                  : "没有可选地点"}
              </option>
              {placeOptions.map((place) => (
                <option
                  key={place.value}
                  value={place.value}
                  disabled={used.has(place.value)}
                >
                  {place.label}{used.has(place.value) ? "（已加入）" : ""}
                </option>
              ))}
            </select>
            <button
              className="secondary-button place-create-trigger"
              type="button"
              aria-label="新增地点并加入当天"
              title={createPlaceDisabledReason || "新增地点并加入当天"}
              disabled={createPlaceDisabled}
              onClick={() => onCreatePlace("当天地点", (option) => {
                setCreatedPlaceOptions((previous) => mergePlaceOptions(
                  previous,
                  [option]
                ));
                setEntries((previous) => previous.some(
                  (entry) => entry.placeId === option.value
                ) ? previous : [...previous, { placeId: option.value }]);
              })}
            >
              <Plus aria-hidden="true" size={18} weight="bold" />
            </button>
          </div>
        </div>
      </section>
      <div className="form-actions">
        <button className="secondary-button" type="button" onClick={onCancel}>取消</button>
        <button className="primary-button" type="submit" disabled={pending || saving || !dirty || dateOutOfRange}>
          {pending || saving ? "保存中…" : "保存"}
        </button>
      </div>
    </form>
  );
}

function mergePlaceOptions(
  primary: FieldOption[],
  extra: FieldOption[]
): FieldOption[] {
  const merged = new Map(primary.map((option) => [option.value, option]));
  extra.forEach((option) => merged.set(option.value, option));
  return [...merged.values()];
}

function editorTitle(editor: Editor | null): string {
  if (!editor) return "编辑当天资料";
  if (editor.kind === "day") return "整理当天地点";
  if (editor.kind === "segment-edit") return "编辑交通段";
  if (editor.kind === "segment-create") return "添加相邻地点交通";
  if (editor.kind === "booking-edit") return "编辑地点预订";
  if (editor.kind === "booking-create") return "添加地点预订";
  if (editor.kind === "research-edit") return "编辑地点攻略";
  return "添加地点攻略";
}

function bookingCreationReason(
  dependencyFailed: boolean,
  expenseTypeCount: number,
  memberCount: number
): string | undefined {
  if (dependencyFailed) return "创建所需的基础资料读取失败";
  if (!memberCount) return "请先为这次旅行选择成员";
  if (!expenseTypeCount) return "请先维护至少一个费用类型";
  return undefined;
}

function researchCreationReason(
  dependencyFailed: boolean,
  platformTypeCount: number,
  memberCount: number
): string | undefined {
  if (dependencyFailed) return "创建所需的基础资料读取失败";
  if (!memberCount) return "请先为这次旅行选择成员";
  if (!platformTypeCount) return "请先维护至少一个平台类型";
  return undefined;
}

function navigationForEntry(
  place: RecordRow<PlaceData> | undefined,
  entry: DayPlaceEntry,
  incoming: RecordRow<SegmentData> | undefined
): string {
  if (!place) return "";
  return generatedNavigationURL({
    id: entry.placeId,
    entry: { id: entry.placeId, entry, place, order: 0 },
    order: 0,
    name: place.data.name,
    arrivalTime: entry.arrivalTime ?? "",
    departureTime: entry.departureTime ?? "",
    additionalInfo: entry.additionalInfo ?? "",
    navigationURL: place.data.navigationURL ?? "",
    longitude: typeof place.data.longitude === "number" ? place.data.longitude : null,
    latitude: typeof place.data.latitude === "number" ? place.data.latitude : null
  } satisfies RoutePoint, incoming?.data.mode ?? "drive");
}

function moveEntry(entries: DayPlaceEntry[], index: number, direction: -1 | 1): DayPlaceEntry[] {
  const next = [...entries];
  const target = index + direction;
  if (!next[index] || !next[target]) return entries;
  [next[index], next[target]] = [next[target]!, next[index]!];
  return next;
}

function updateEntry(
  entries: DayPlaceEntry[],
  index: number,
  field: keyof DayPlaceEntry,
  value: string
): DayPlaceEntry[] {
  return entries.map((entry, itemIndex) => itemIndex === index ? { ...entry, [field]: value } : entry);
}

function errorText(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback;
}
