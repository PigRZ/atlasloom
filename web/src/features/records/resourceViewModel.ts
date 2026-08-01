// 资源页模型集中处理筛选、创建前置条件和费用成员归一化。
import type {
  DayData,
  PageResource,
  RecordRow,
  Trip
} from "../../lib/types";

export type ExpenseRecordState = "pending" | "ready";

export function filterResourceRecords(
  rows: RecordRow[],
  resource: "places" | "bookings" | "expenses",
  filters: {
    typeID?: string;
    memberID?: string;
    dayID?: string;
    expenseState?: ExpenseRecordState | "";
  },
  days: Array<RecordRow<DayData>> = []
): RecordRow[] {
  const dayPlaceIDs = new Set(
    filters.dayID
      ? days
        .filter((day) => day.id === filters.dayID)
        .flatMap((day) => (day.data.placeEntries ?? []).map((entry) => entry.placeId))
      : []
  );
  return rows.filter((row) => {
    if (filters.typeID && String(row.data.type ?? "") !== filters.typeID) return false;
    if (
      resource === "bookings"
      && filters.memberID
      && String(row.data.bookerMemberId ?? "") !== filters.memberID
    ) return false;
    if (resource === "places" && filters.dayID && !dayPlaceIDs.has(row.id)) return false;
    if (
      resource === "expenses"
      && filters.expenseState
      && expenseRecordState(row) !== filters.expenseState
    ) return false;
    return true;
  });
}

export function expenseRecordState(row: RecordRow): ExpenseRecordState {
  const amount = Number(row.data.amountMinor ?? 0);
  return Number.isFinite(amount)
    && amount > 0
    && Boolean(String(row.data.payerMemberId ?? ""))
    ? "ready"
    : "pending";
}

export function filterResearchRecords(
  rows: RecordRow[],
  dayID: string,
  placeID: string
): RecordRow[] {
  return rows.filter((row) => {
    const dayIDs = Array.isArray(row.data.dayIds) ? row.data.dayIds.map(String) : [];
    return (!dayID || dayIDs.includes(dayID))
      && (!placeID || String(row.data.placeId ?? "") === placeID);
  });
}

export function filterPackingRecords(rows: RecordRow[], dayID: string): RecordRow[] {
  if (!dayID) return rows;
  return rows.filter((row) => (
    Array.isArray(row.data.dayIds)
    && row.data.dayIds.map(String).includes(dayID)
  ));
}

export function normalizeExpenseParticipants(
  resource: PageResource,
  data: Record<string, unknown>,
  members: RecordRow[]
): Record<string, unknown> {
  if (resource !== "expenses" || data.splitMode === "selected") return data;
  return {
    ...data,
    splitMemberIds: members.map((member) => String(member.data.memberId ?? member.id))
  };
}

export function localMinute(): string {
  const now = new Date();
  const offset = now.getTimezoneOffset() * 60_000;
  return new Date(now.getTime() - offset).toISOString().slice(0, 16);
}

export function tripMemberDeletionConfirmation(
  memberID: string,
  currentMemberID: string,
  currentRole: Trip["role"]
): string {
  if (memberID === currentMemberID && currentRole === "owner") {
    return "删除后，你仍保留负责人访问权限，但不再作为同行成员参与付款、分摊或资料记录。此操作不能撤销。";
  }
  if (memberID === currentMemberID) {
    return "删除后，你将不再参与这次旅行，并失去访问权限。此操作不能撤销。";
  }
  return "删除后，该成员不再参与这次旅行；其非负责人访问权限也会撤销。此操作不能撤销。";
}

type DependencyStatus = {
  rows: boolean;
  members: boolean;
  globalMembers: boolean;
  tags: boolean;
};

type CreationState = {
  tripId: string;
  placeTypeCount: number;
  expenseTypeCount: number;
  platformTypeCount: number;
  selectedMemberCount: number;
  availableMemberCount: number;
};

export type ResourceCreationBlocker = {
  message: string;
  actionLabel: string;
  href: string;
};

export function creationDependenciesReady(
  resource: PageResource,
  status: DependencyStatus
): boolean {
  if (resource === "places") return status.tags;
  if (resource === "bookings" || resource === "expenses" || resource === "research-evidence") {
    return status.members && status.globalMembers && status.tags;
  }
  if (resource === "members") return status.rows && status.globalMembers;
  return true;
}

export function creationDependenciesFailed(
  resource: PageResource,
  status: DependencyStatus
): boolean {
  if (resource === "places") return status.tags;
  if (resource === "bookings" || resource === "expenses" || resource === "research-evidence") {
    return status.members || status.globalMembers || status.tags;
  }
  if (resource === "members") return status.rows || status.globalMembers;
  return false;
}

export function resourceCreationBlocker(
  resource: PageResource,
  state: CreationState
): ResourceCreationBlocker | null {
  if (resource === "places" && state.placeTypeCount === 0) {
    return {
      message: "添加地点前，需要先维护至少 1 个地点类型。",
      actionLabel: "维护地点类型",
      href: "/app/library?section=tags"
    };
  }
  if (resource === "members" && state.availableMemberCount === 0) {
    return {
      message: "基础资料中没有可加入这次旅行的新成员。",
      actionLabel: "维护基础成员",
      href: "/app/library?section=members"
    };
  }
  if (
    ["bookings", "expenses", "research-evidence"].includes(resource)
    && state.selectedMemberCount === 0
  ) {
    return {
      message: "先为这次旅行选择成员，才能记录支付人、分摊或资料记录人。",
      actionLabel: "选择旅行成员",
      href: `/app/trips/${state.tripId}/members`
    };
  }
  if (resource === "bookings" && state.expenseTypeCount === 0) {
    return {
      message: "添加预订前，需要先维护至少 1 个费用类型，作为预订类型使用。",
      actionLabel: "维护费用类型",
      href: "/app/library?section=tags"
    };
  }
  if (resource === "expenses" && state.expenseTypeCount === 0) {
    return {
      message: "添加费用前，需要先维护至少 1 个费用类型。",
      actionLabel: "维护费用类型",
      href: "/app/library?section=tags"
    };
  }
  if (resource === "research-evidence" && state.platformTypeCount === 0) {
    return {
      message: "添加资料前，需要先维护至少 1 个平台类型。",
      actionLabel: "维护平台类型",
      href: "/app/library?section=tags"
    };
  }
  return null;
}
