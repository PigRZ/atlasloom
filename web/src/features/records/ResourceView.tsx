// 资源页只维护当前模型中的资料；地点类型与费用类型统一来自标签。
import { FilePlus, Plus } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { EditorSurface } from "../../components/EditorSurface";
import { useConfirm } from "../../components/ConfirmDialog";
import { DataError } from "../../components/PageStates";
import { api } from "../../lib/api";
import {
  tripMemberCascadeConfirmation,
  tripMemberDeletionBlockedMessage,
} from "../../lib/deletionImpact";
import { resources } from "../../lib/resourceDefinitions";
import type {
  DayData,
  LibraryRecord,
  PageResource,
  RecordRow,
  SnapshotSummary,
  Trip,
  TripMemberDeletionImpact
} from "../../lib/types";
import { MemberSettlement } from "./ExpensePanels";
import { RecordCard } from "./RecordCard";
import { PlaceQuickCreate } from "./PlaceQuickCreate";
import {
  RecordForm,
  type FieldOption,
  type PlaceCreationRequest
} from "./RecordForm";
import {
  creationDependenciesFailed,
  creationDependenciesReady,
  expenseRecordState,
  filterPackingRecords,
  filterResearchRecords,
  filterResourceRecords,
  localMinute,
  normalizeExpenseParticipants,
  resourceCreationBlocker,
  tripMemberDeletionConfirmation,
  type ExpenseRecordState
} from "./resourceViewModel";

type ResourceViewProps = {
  trip: Trip;
  resource: PageResource;
  currentMemberId: string;
  setError: (message: string) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
};

type Editor =
  | { kind: "create"; defaults: Record<string, unknown>; idempotencyKey: string }
  | { kind: "edit"; row: RecordRow };

type PlaceCreator = {
  targetLabel: string;
  idempotencyKey: string;
  onCreated: Parameters<PlaceCreationRequest>[1];
};

const emptyRows: RecordRow[] = [];
const emptyLibraryRows: LibraryRecord[] = [];
const expenseStateTabs: Array<{
  value: ExpenseRecordState | "";
  label: string;
}> = [
  { value: "", label: "全部" },
  { value: "pending", label: "待补录" },
  { value: "ready", label: "已登记" }
];

export function ResourceView({
  trip,
  resource,
  currentMemberId,
  setError,
  onSnapshot,
  onDirtyChange
}: ResourceViewProps) {
  const definition = resources.find((item) => item.id === resource)!;
  const readOnly = trip.role === "viewer";
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const [searchParams, setSearchParams] = useSearchParams();
  const [editor, setEditor] = useState<Editor | null>(null);
  const [formDirty, setFormDirty] = useState(false);
  const [placeFormDirty, setPlaceFormDirty] = useState(false);
  const [placeCreator, setPlaceCreator] = useState<PlaceCreator | null>(null);
  const [savedRecordId, setSavedRecordId] = useState("");

  const rows = useQuery({
    queryKey: resource === "packing-items"
      ? ["records", trip.id, resource, currentMemberId]
      : ["records", trip.id, resource],
    queryFn: () => api.records(trip.id, resource)
  });
  const needsMembers = [
    "expenses", "bookings", "research-evidence", "members"
  ].includes(resource);
  const members = useQuery({
    queryKey: ["records", trip.id, "members"],
    queryFn: () => api.records(trip.id, "members"),
    enabled: needsMembers
  });
  const globalMembers = useQuery({
    queryKey: ["library", "members"],
    queryFn: () => api.libraryRecords("members"),
    enabled: needsMembers || resource === "places"
  });
  const tags = useQuery({
    queryKey: ["library", "tags"],
    queryFn: () => api.libraryRecords("tags"),
    enabled: ["places", "bookings", "expenses", "research-evidence"].includes(resource)
  });
  const days = useQuery({
    queryKey: ["records", trip.id, "days"],
    queryFn: () => api.records(trip.id, "days"),
    enabled: ["places", "packing-items", "research-evidence"].includes(resource)
  });
  const places = useQuery({
    queryKey: ["records", trip.id, "places"],
    queryFn: () => api.records(trip.id, "places"),
    enabled: ["bookings", "expenses", "research-evidence"].includes(resource)
  });
  const segments = useQuery({
    queryKey: ["records", trip.id, "segments"],
    queryFn: () => api.records(trip.id, "segments"),
    enabled: resource === "places"
  });
  const relatedExpenses = useQuery({
    queryKey: ["records", trip.id, "expenses"],
    queryFn: () => api.records(trip.id, "expenses"),
    enabled: resource === "bookings"
  });
  const relatedBookings = useQuery({
    queryKey: ["records", trip.id, "bookings"],
    queryFn: () => api.records(trip.id, "bookings"),
    enabled: resource === "expenses"
  });

  const recordRows = rows.data?.records ?? emptyRows;
  const typeFilter = ["places", "bookings", "expenses"].includes(resource)
    ? searchParams.get("type") ?? ""
    : "";
  const bookerFilter = resource === "bookings"
    ? searchParams.get("member") ?? ""
    : "";
  const placeDayFilter = resource === "places"
    ? searchParams.get("day") ?? ""
    : "";
  const requestedExpenseState = resource === "expenses" ? searchParams.get("state") : "";
  const expenseStateFilter: ExpenseRecordState | "" = requestedExpenseState === "pending"
    || requestedExpenseState === "ready"
    ? requestedExpenseState
    : "";
  const researchDayFilter = resource === "research-evidence"
    ? searchParams.get("day") ?? ""
    : "";
  const researchPlaceFilter = resource === "research-evidence"
    ? searchParams.get("place") ?? ""
    : "";
  const packingDayFilter = resource === "packing-items"
    ? searchParams.get("day") ?? ""
    : "";
  const researchFilterActive = Boolean(researchDayFilter || researchPlaceFilter);
  const tripMemberRows = resource === "members" ? recordRows : members.data?.records ?? emptyRows;
  const globalMemberRows = globalMembers.data?.records ?? emptyLibraryRows;
  const globalMemberNames = useMemo(() => new Map(globalMemberRows.map((member) => [
    member.id,
    String(member.data.displayName ?? member.recordKey)
  ])), [globalMemberRows]);
  const hydratedMembers = tripMemberRows.map((member) => {
    const memberID = String(member.data.memberId ?? member.id);
    return {
      ...member,
      data: {
        ...member.data,
        memberId: memberID,
        displayName: globalMemberNames.get(memberID) ?? "未知成员"
      }
    };
  });
  const expenseRows = resource === "expenses"
    ? recordRows
    : relatedExpenses.data?.records ?? emptyRows;
  const bookingRows = resource === "bookings"
    ? recordRows
    : relatedBookings.data?.records ?? emptyRows;
  const requestedExpense = !readOnly && resource === "expenses"
    ? recordRows.find((row) => row.id === searchParams.get("edit"))
    : undefined;
  const activeEditor: Editor | null = readOnly
    ? null
    : editor ?? (requestedExpense ? { kind: "edit", row: requestedExpense } : null);

  const invalidate = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ["records", trip.id] }),
    queryClient.invalidateQueries({ queryKey: ["overview", trip.id] }),
    queryClient.invalidateQueries({ queryKey: ["audit-logs", trip.id] }),
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
    }) => api.createRecord(
      trip.id,
      resource,
      recordKey,
      normalizeExpenseParticipants(resource, data, tripMemberRows),
      idempotencyKey
    ),
    onSuccess: async ({ record, snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      setSavedRecordId(record.id);
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "保存失败"))
  });
  const update = useMutation({
    mutationFn: ({ row, data }: { row: RecordRow; data: Record<string, unknown> }) => (
      api.updateRecord(
        trip.id,
        resource,
        row,
        normalizeExpenseParticipants(resource, data, tripMemberRows)
      )
    ),
    onSuccess: async ({ record, snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      setSavedRecordId(record.id);
      closeEditor();
    },
    onError: (reason) => setError(errorText(reason, "保存失败"))
  });
  const remove = useMutation({
    mutationFn: ({ row, cascade }: { row: RecordRow; cascade: boolean }) => (
      api.deleteRecord(trip.id, resource, row, cascade)
    ),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
    },
    onError: (reason) => setError(errorText(reason, "删除失败"))
  });

  const closeEditor = useCallback(() => {
    setEditor(null);
    setPlaceCreator(null);
    if (searchParams.has("edit")) {
      const next = new URLSearchParams(searchParams);
      next.delete("edit");
      setSearchParams(next, { replace: true });
    }
    setFormDirty(false);
    setPlaceFormDirty(false);
  }, [searchParams, setSearchParams]);
  const handleDirtyChange = useCallback((dirty: boolean) => {
    setFormDirty(dirty);
  }, []);
  const requestPlaceCreation = useCallback<PlaceCreationRequest>((
    targetLabel,
    onCreated
  ) => {
    setPlaceCreator({
      targetLabel,
      onCreated,
      idempotencyKey: crypto.randomUUID()
    });
  }, []);
  const closePlaceCreator = useCallback(() => {
    setPlaceCreator(null);
    setPlaceFormDirty(false);
  }, []);
  const cancelEditor = useCallback(async () => {
    if (!formDirty || await confirm({
      title: "放弃未保存修改？",
      message: "当前表单有未保存的修改。",
      confirmLabel: "放弃修改",
      tone: "danger"
    })) closeEditor();
  }, [closeEditor, confirm, formDirty]);

  useEffect(() => {
    onDirtyChange(formDirty || placeFormDirty);
  }, [formDirty, onDirtyChange, placeFormDirty]);
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  useEffect(() => {
    if (!savedRecordId) return undefined;
    const timer = window.setTimeout(() => setSavedRecordId(""), 2400);
    return () => window.clearTimeout(timer);
  }, [savedRecordId]);
  const formOptions = useMemo<Record<string, FieldOption[]>>(() => {
    const tagRows = tags.data?.records ?? [];
    const activeTags = (category: string) => tagRows
      .filter((tag) => String(tag.data.status ?? "active") === "active")
      .filter((tag) => String(tag.data.category ?? "general") === category)
      .map((tag) => ({ value: tag.id, label: String(tag.data.name ?? tag.recordKey) }));
    const selectedMembers = tripMemberRows.map((member) => {
      const memberID = String(member.data.memberId ?? member.id);
      return { value: memberID, label: globalMemberNames.get(memberID) ?? member.recordKey };
    });
    return {
      memberId: globalMemberRows
        .filter((member) => String(member.data.status ?? "active") === "active")
        .map((member) => ({ value: member.id, label: String(member.data.displayName ?? member.recordKey) })),
      payerMemberId: selectedMembers,
      bookerMemberId: selectedMembers,
      recordedByMemberId: selectedMembers,
      splitMemberIds: selectedMembers,
      dayIds: (days.data?.records ?? []).map((day) => ({
        value: day.id,
        label: `${String(day.data.date ?? "")} · ${String(day.data.title ?? day.recordKey)}`
      })),
      placeId: (places.data?.records ?? []).map((place) => ({
        value: place.id,
        label: String(place.data.name ?? place.recordKey)
      })),
      type: resource === "places" ? activeTags("place-type")
        : resource === "expenses" || resource === "bookings" ? activeTags("expense-type")
          : [],
      platformId: activeTags("platform-type")
    };
  }, [
    days.data?.records,
    globalMemberNames,
    globalMemberRows,
    places.data?.records,
    resource,
    tags.data?.records,
    tripMemberRows
  ]);
  const filterTypeCategory = resource === "places" ? "place-type"
    : resource === "bookings" || resource === "expenses" ? "expense-type"
      : "";
  const filterTypeOptions = (tags.data?.records ?? [])
    .filter((tag) => String(tag.data.category ?? "general") === filterTypeCategory)
    .map((tag) => ({
      value: tag.id,
      label: String(tag.data.name ?? tag.recordKey)
    }));
  const displayLabels = useMemo(() => new Map([
    ...Object.values(formOptions).flat().map(
      (option): [string, string] => [option.value, option.label]
    ),
    ...(tags.data?.records ?? []).map(
      (tag): [string, string] => [tag.id, String(tag.data.name ?? tag.recordKey)]
    )
  ]), [formOptions, tags.data?.records]);
  const recordFilterActive = Boolean(
    typeFilter || bookerFilter || placeDayFilter || expenseStateFilter || packingDayFilter
  );
  const visibleRecordRows = resource === "research-evidence"
    ? filterResearchRecords(recordRows, researchDayFilter, researchPlaceFilter)
    : resource === "packing-items"
      ? filterPackingRecords(recordRows, packingDayFilter)
    : ["places", "bookings", "expenses"].includes(resource)
      ? filterResourceRecords(
        recordRows,
        resource as "places" | "bookings" | "expenses",
        {
          typeID: typeFilter,
          memberID: bookerFilter,
          dayID: placeDayFilter,
          expenseState: expenseStateFilter
        },
        days.data?.records as Array<RecordRow<DayData>> | undefined
      )
      : recordRows;
  const expenseRowsForTabs = resource === "expenses"
    ? filterResourceRecords(
      recordRows,
      "expenses",
      { typeID: typeFilter, memberID: bookerFilter, dayID: placeDayFilter },
      days.data?.records as Array<RecordRow<DayData>> | undefined
    )
    : emptyRows;
  const filterActive = researchFilterActive || recordFilterActive;
  const primaryField = definition.fields[0]?.[0] ?? "recordKey";
  const activeTagRows = (tags.data?.records ?? []).filter(
    (tag) => String(tag.data.status ?? "active") === "active"
  );
  const placeTypeOptions = activeTagRows
    .filter((tag) => String(tag.data.category ?? "") === "place-type")
    .map((tag) => ({
      value: tag.id,
      label: String(tag.data.name ?? tag.recordKey)
    }));
  const placeCreationDisabled = tags.isLoading
    || tags.isError
    || placeTypeOptions.length === 0;
  const placeCreationDisabledReason = tags.isError
    ? "地点类型读取失败，请刷新后重试"
    : placeTypeOptions.length === 0 && tags.isSuccess
      ? "请先在基础资料中维护地点类型"
      : tags.isLoading
        ? "正在读取地点类型"
        : undefined;
  const selectedMemberIDs = new Set(
    tripMemberRows.map((member) => String(member.data.memberId ?? member.id))
  );
  const defaultBookerMemberID = selectedMemberIDs.has(currentMemberId)
    ? currentMemberId
    : String(tripMemberRows[0]?.data.memberId ?? tripMemberRows[0]?.id ?? "");
  const dependenciesReady = creationDependenciesReady(resource, {
    rows: rows.isSuccess,
    members: members.isSuccess,
    globalMembers: globalMembers.isSuccess,
    tags: tags.isSuccess
  });
  const dependenciesFailed = creationDependenciesFailed(resource, {
    rows: rows.isError,
    members: members.isError,
    globalMembers: globalMembers.isError,
    tags: tags.isError
  });
  const blocker = dependenciesReady
    ? resourceCreationBlocker(resource, {
      tripId: trip.id,
      placeTypeCount: activeTagRows.filter(
        (tag) => String(tag.data.category ?? "") === "place-type"
      ).length,
      expenseTypeCount: activeTagRows.filter(
        (tag) => String(tag.data.category ?? "") === "expense-type"
      ).length,
      platformTypeCount: activeTagRows.filter(
        (tag) => String(tag.data.category ?? "") === "platform-type"
      ).length,
      selectedMemberCount: selectedMemberIDs.size,
      availableMemberCount: globalMemberRows.filter(
        (member) => String(member.data.status ?? "active") === "active"
          && !selectedMemberIDs.has(member.id)
      ).length
    })
    : null;
  const createDisabled = !dependenciesReady || dependenciesFailed || blocker !== null;

  return (
    <div className="resource-page page-enter">
      <section className="page-heading">
        <div><h1>{definition.label}</h1></div>
        {!readOnly && (
          <button
            className="primary-button"
            type="button"
            disabled={createDisabled}
            title={blocker?.message ?? (dependenciesFailed ? "创建所需的基础资料读取失败" : undefined)}
            onClick={() => setEditor(newResourceEditor(
              resource === "expenses"
                ? { splitMode: "all", occurredAt: localMinute() }
                : resource === "bookings"
                  ? { bookerMemberId: defaultBookerMemberID }
                : resource === "research-evidence"
                  ? { recordedByMemberId: currentMemberId }
                  : resource === "packing-items" && packingDayFilter
                    ? { dayIds: [packingDayFilter] }
                  : {}
            ))}
          >
            <Plus aria-hidden="true" size={17} weight="bold" />
            新增{definition.singular}
          </button>
        )}
      </section>
      {readOnly && (
        <p className="readonly-notice" role="status">
          你拥有本次旅行的只读权限，可以查看资料，但不能新增、编辑或删除。
        </p>
      )}
      {!readOnly && dependenciesFailed && (
        <p className="setup-notice" role="alert">
          创建所需的基础资料读取失败，请刷新页面后重试。
        </p>
      )}
      {!readOnly && blocker && (
        <aside className="setup-notice" aria-label="创建前准备">
          <span>{blocker.message}</span>
          <Link to={blocker.href}>{blocker.actionLabel}</Link>
        </aside>
      )}

      {resource === "expenses" && (
        <MemberSettlement
          members={hydratedMembers}
          expenses={expenseRows}
          currency={trip.currency}
          onOpenExpense={readOnly
            ? undefined
            : (expense) => setEditor({ kind: "edit", row: expense })}
        />
      )}

      <section className="content-grid">
        <div className="records-panel">
          <div className="panel-heading">
            <div>
              <h2>{resource === "expenses" ? "费用记录" : "全部记录"}</h2>
              <span>
                {filterActive
                  ? `${visibleRecordRows.length} / ${recordRows.length} 条`
                  : `${recordRows.length} 条`}
              </span>
            </div>
          </div>
          {resource === "expenses" && (
            <div className="expense-state-tabs" role="group" aria-label="按登记状态筛选费用">
              {expenseStateTabs.map((tab) => {
                const count = tab.value
                  ? expenseRowsForTabs.filter(
                    (row) => expenseRecordState(row) === tab.value
                  ).length
                  : expenseRowsForTabs.length;
                const active = expenseStateFilter === tab.value;
                return (
                  <button
                    key={tab.value || "all"}
                    className={active ? "is-active" : undefined}
                    type="button"
                    aria-pressed={active}
                    onClick={() => updateURLFilter(
                      searchParams,
                      setSearchParams,
                      "state",
                      tab.value
                    )}
                  >
                    <span>{tab.label}</span><b>{count}</b>
                  </button>
                );
              })}
            </div>
          )}
          {resource === "research-evidence" && (
            <div className="research-filters" role="group" aria-label="资料筛选">
              <label>
                <span>按执行日</span>
                <select
                  name="research-day-filter"
                  value={researchDayFilter}
                  onChange={(event) => updateURLFilter(
                    searchParams,
                    setSearchParams,
                    "day",
                    event.target.value
                  )}
                >
                  <option value="">全部执行日</option>
                  {(days.data?.records ?? []).map((day) => (
                    <option key={day.id} value={day.id}>
                      {String(day.data.date ?? "")} · {String(day.data.title ?? day.recordKey)}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>按地点</span>
                <select
                  name="research-place-filter"
                  value={researchPlaceFilter}
                  onChange={(event) => updateURLFilter(
                    searchParams,
                    setSearchParams,
                    "place",
                    event.target.value
                  )}
                >
                  <option value="">全部地点</option>
                  {(places.data?.records ?? []).map((place) => (
                    <option key={place.id} value={place.id}>
                      {String(place.data.name ?? place.recordKey)}
                    </option>
                  ))}
                </select>
              </label>
              <button
                className="secondary-button"
                type="button"
                disabled={!researchFilterActive}
                onClick={() => clearResearchFilters(searchParams, setSearchParams)}
              >
                清除筛选
              </button>
            </div>
          )}
          {resource === "packing-items" && (
            <div className="research-filters packing-filters" role="group" aria-label="物品筛选">
              <label>
                <span>按执行日</span>
                <select
                  name="packing-day-filter"
                  value={packingDayFilter}
                  onChange={(event) => updateURLFilter(
                    searchParams,
                    setSearchParams,
                    "day",
                    event.target.value
                  )}
                >
                  <option value="">全部执行日</option>
                  {(days.data?.records ?? []).map((day) => (
                    <option key={day.id} value={day.id}>
                      {String(day.data.date ?? "")} · {String(day.data.title ?? day.recordKey)}
                    </option>
                  ))}
                </select>
              </label>
              <button
                className="secondary-button"
                type="button"
                disabled={!packingDayFilter}
                onClick={() => updateURLFilter(searchParams, setSearchParams, "day", "")}
              >
                清除筛选
              </button>
            </div>
          )}
          {["places", "bookings", "expenses"].includes(resource) && (
            <div className="research-filters resource-filters" role="group" aria-label={`${definition.label}筛选`}>
              <label>
                <span>
                  {resource === "places"
                    ? "按地点类型"
                    : resource === "bookings" ? "按预订类型" : "按费用类型"}
                </span>
                <select
                  name={`${resource}-type-filter`}
                  value={typeFilter}
                  onChange={(event) => updateURLFilter(
                    searchParams,
                    setSearchParams,
                    "type",
                    event.target.value
                  )}
                >
                  <option value="">全部类型</option>
                  {filterTypeOptions.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
              {resource === "bookings" && (
                <label>
                  <span>按预订人</span>
                  <select
                    name="booking-member-filter"
                    value={bookerFilter}
                    onChange={(event) => updateURLFilter(
                      searchParams,
                      setSearchParams,
                      "member",
                      event.target.value
                    )}
                  >
                    <option value="">全部预订人</option>
                    {(formOptions.bookerMemberId ?? []).map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                </label>
              )}
              {resource === "places" && (
                <label>
                  <span>按关联日期</span>
                  <select
                    name="place-day-filter"
                    value={placeDayFilter}
                    onChange={(event) => updateURLFilter(
                      searchParams,
                      setSearchParams,
                      "day",
                      event.target.value
                    )}
                  >
                    <option value="">全部日期</option>
                    {(days.data?.records ?? []).map((day) => (
                      <option key={day.id} value={day.id}>
                        {String(day.data.date ?? "")} · {String(day.data.title ?? day.recordKey)}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              <button
                className="secondary-button"
                type="button"
                disabled={!recordFilterActive}
                onClick={() => clearResourceFilters(
                  searchParams,
                  setSearchParams,
                  resource as "places" | "bookings" | "expenses"
                )}
              >
                清除筛选
              </button>
            </div>
          )}
          {rows.isLoading ? (
            <div className="skeleton-block" role="status"><span className="sr-only">正在读取…</span></div>
          ) : rows.isError ? (
            <DataError message={`${definition.label}读取失败`} onRetry={() => rows.refetch()} />
          ) : visibleRecordRows.length ? (
            <div className="record-list">
              {visibleRecordRows.map((row) => {
                const impact = resource === "places"
                  ? placeImpact(row, days.data?.records as Array<RecordRow<DayData>> | undefined, segments.data?.records)
                  : undefined;
                const linkedExpense = resource === "bookings"
                  ? expenseRows.find((expense) => String(expense.data.bookingRecordId ?? "") === row.id)
                  : undefined;
                const generatedExpense = resource === "expenses" && Boolean(row.data.bookingRecordId);
                const linkedBooking = generatedExpense
                  ? bookingRows.find((booking) => booking.id === String(row.data.bookingRecordId))
                  : undefined;
                return (
                  <RecordCard
                    key={row.id}
                    row={row}
                    currency={trip.currency}
                    definition={definition}
                    displayLabels={displayLabels}
                    highlighted={savedRecordId === row.id}
                    audit={resource === "places" ? {
                      createdBy: globalMemberNames.get(row.createdByMemberId) ?? "未知成员",
                      updatedBy: globalMemberNames.get(row.updatedByMemberId) ?? "未知成员"
                    } : undefined}
                    linkedExpense={linkedExpense}
                    linkedExpenseHref={linkedExpense
                      ? readOnly
                        ? `/app/trips/${trip.id}/expenses`
                        : `/app/trips/${trip.id}/expenses?edit=${encodeURIComponent(linkedExpense.id)}`
                      : undefined}
                    linkedBooking={linkedBooking}
                    readOnly={readOnly}
                    hideDelete={generatedExpense}
                    onEdit={async () => {
                      if (
                        resource !== "places"
                        || !impact?.dayCount
                        || await confirm({
                          title: `修改地点“${String(row.data.name ?? row.recordKey)}”？`,
                          message: `${impact.summary}\n\n修改位置或类型会同步影响这些日程的天气与导航。`,
                          confirmLabel: "继续修改"
                        })
                      ) setEditor({ kind: "edit", row });
                    }}
                    onDelete={async () => {
                      if (generatedExpense) return;
                      const primaryValue = String(row.data[primaryField] ?? "");
                      const title = displayLabels.get(primaryValue)
                        ?? (primaryValue || row.recordKey);
                      let cascadeMemberImpact: TripMemberDeletionImpact | null = null;
                      if (resource === "members") {
                        try {
                          const deletionImpact = await api.tripMemberDeletionImpact(
                            trip.id,
                            row
                          );
                          if (deletionImpact.references.length > 0) {
                            await confirm({
                              title: `暂时不能删除同行成员“${title}”`,
                              message: tripMemberDeletionBlockedMessage(
                                title,
                                deletionImpact
                              ),
                              confirmLabel: "知道了",
                              tone: "danger",
                              acknowledgeOnly: true
                            });
                            return;
                          }
                          if (
                            deletionImpact.hasPrivatePackingItems
                            || deletionImpact.cascadeExpenses.length > 0
                          ) {
                            cascadeMemberImpact = deletionImpact;
                          }
                        } catch (reason) {
                          setError(errorText(reason, "无法检查该成员被哪些资料使用，请稍后重试。"));
                          return;
                        }
                      }
                      const message = resource === "places"
                        ? [
                          impact?.summary || "当前没有关联日程。",
                          impact?.dayCount
                            ? `删除后将从 ${impact.dayCount} 个执行日移除此地点，并删除 ${impact.segmentCount} 条关联交通段。`
                            : "",
                          "预订与资料会保留，但解除地点关联。此操作不能撤销。"
                        ].filter(Boolean).join("\n\n")
                        : resource === "bookings"
                          ? "关联的待登记费用会同时删除。此操作不能撤销。"
                          : resource === "members"
                            ? cascadeMemberImpact
                              ? tripMemberCascadeConfirmation(title, cascadeMemberImpact)
                              : tripMemberDeletionConfirmation(
                                String(row.data.memberId ?? ""),
                                currentMemberId,
                                trip.role
                              )
                          : "此操作不能撤销。";
                      if (await confirm({
                        title: cascadeMemberImpact
                          ? `删除同行成员“${title}”并清理关联资料？`
                          : `删除${definition.singular}“${title}”？`,
                        message,
                        confirmLabel: cascadeMemberImpact
                          ? "删除成员和关联资料"
                          : "确认删除",
                        tone: "danger"
                      })) remove.mutate({
                        row,
                        cascade: resource === "places" || cascadeMemberImpact !== null
                      });
                    }}
                  />
                );
              })}
            </div>
          ) : (
            <div className="empty-inline">
              <FilePlus aria-hidden="true" size={30} weight="duotone" />
              <b>
                {filterActive ? `没有符合筛选条件的${definition.singular}` : `暂无${definition.singular}`}
              </b>
            </div>
          )}
        </div>
      </section>

      <EditorSurface
        open={activeEditor !== null}
        title={`${activeEditor?.kind === "edit" ? "编辑" : "新增"}${definition.singular}`}
        variant={definition.fields.length > 7 ? "drawer" : "modal"}
        dirty={formDirty}
        onClose={closeEditor}
      >
        {activeEditor && (
          <RecordForm
            tripId={trip.id}
            definition={definition}
            initial={activeEditor.kind === "edit" ? activeEditor.row.data : undefined}
            defaults={activeEditor.kind === "create" ? activeEditor.defaults : undefined}
            options={formOptions}
            hiddenFields={
              resource === "expenses"
              && activeEditor.kind === "edit"
              && Boolean(activeEditor.row.data.bookingRecordId)
                ? ["item"]
                : undefined
            }
            readOnlyFields={
              resource === "expenses"
              && activeEditor.kind === "edit"
              && Boolean(activeEditor.row.data.bookingRecordId)
                ? ["type", "payerMemberId"]
                : undefined
            }
            contextNote={
              resource === "expenses"
              && activeEditor.kind === "edit"
              && Boolean(activeEditor.row.data.bookingRecordId)
                ? "这笔费用由预订自动创建，项目、费用类型与支付人跟随来源预订；请回到预订修改或删除。"
                : undefined
            }
            onCreatePlace={requestPlaceCreation}
            createPlaceDisabled={placeCreationDisabled}
            createPlaceDisabledReason={placeCreationDisabledReason}
            onCancel={cancelEditor}
            onDirtyChange={handleDirtyChange}
            onSubmit={(recordKey, data) => activeEditor.kind === "edit"
              ? update.mutateAsync({ row: activeEditor.row, data }).then(() => undefined)
              : create.mutateAsync({ recordKey, data, idempotencyKey: activeEditor.idempotencyKey }).then(() => undefined)}
            pending={create.isPending || update.isPending}
          />
        )}
      </EditorSurface>
      {!readOnly && placeCreator && (
        <PlaceQuickCreate
          tripId={trip.id}
          targetLabel={placeCreator.targetLabel}
          idempotencyKey={placeCreator.idempotencyKey}
          placeTypeOptions={placeTypeOptions}
          onSnapshot={onSnapshot}
          onCreated={placeCreator.onCreated}
          onDirtyChange={setPlaceFormDirty}
          onClose={closePlaceCreator}
        />
      )}
    </div>
  );
}

function placeImpact(
  place: RecordRow,
  days: Array<RecordRow<DayData>> = [],
  segments: RecordRow[] = []
): { dayCount: number; segmentCount: number; summary: string } {
  const affectedDays = days.filter((day) => (
    (day.data.placeEntries ?? []).some((entry) => entry.placeId === place.id)
  ));
  const affectedSegments = segments.filter((segment) => (
    String(segment.data.fromPlaceId ?? "") === place.id
    || String(segment.data.toPlaceId ?? "") === place.id
  ));
  const lines = affectedDays.map((day) => `• ${day.data.date} · ${day.data.title}`);
  return {
    dayCount: affectedDays.length,
    segmentCount: affectedSegments.length,
    summary: lines.length ? `关联日程：\n${lines.join("\n")}` : ""
  };
}

function newResourceEditor(defaults: Record<string, unknown> = {}): Extract<Editor, { kind: "create" }> {
  return { kind: "create", defaults, idempotencyKey: crypto.randomUUID() };
}

function updateURLFilter(
  current: URLSearchParams,
  setSearchParams: ReturnType<typeof useSearchParams>[1],
  key: "day" | "place" | "type" | "member" | "state",
  value: string
) {
  const next = new URLSearchParams(current);
  if (value) next.set(key, value);
  else next.delete(key);
  setSearchParams(next, { replace: true });
}

function clearResearchFilters(
  current: URLSearchParams,
  setSearchParams: ReturnType<typeof useSearchParams>[1]
) {
  const next = new URLSearchParams(current);
  next.delete("day");
  next.delete("place");
  setSearchParams(next, { replace: true });
}

function clearResourceFilters(
  current: URLSearchParams,
  setSearchParams: ReturnType<typeof useSearchParams>[1],
  resource: "places" | "bookings" | "expenses"
) {
  const next = new URLSearchParams(current);
  next.delete("type");
  if (resource === "bookings") next.delete("member");
  if (resource === "places") next.delete("day");
  if (resource === "expenses") next.delete("state");
  setSearchParams(next, { replace: true });
}

function errorText(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback;
}
