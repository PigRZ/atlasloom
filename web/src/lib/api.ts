// 浏览器 API 客户端统一同源地址、会话 Cookie、CSRF、幂等键和并发修订头。
import type {
  AuditLog,
  CreateTripInput,
  MapConfig,
  LibraryDeletionImpact,
  LibraryRecord,
  LibraryResource,
  LibraryState,
  PlaceCandidate,
  RecordRow,
  Resource,
  RoutePlan,
  Session,
  SnapshotSummary,
  Trip,
  TripBackupDependencyMapping,
  TripBackupImportPreview,
  TripMemberDeletionImpact,
  UpdateTripInput,
  WeatherResponse
} from "./types";

const apiBase = `${import.meta.env.BASE_URL.replace(/\/$/, "")}/api`;
let csrfToken = "";

export function setCSRFToken(token: string) { csrfToken = token; }

export function arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

export function snapshotMirrorWarning(
  snapshot?: Pick<SnapshotSummary, "mirrorState">
): string {
  return snapshot?.mirrorState === "pending"
    ? "数据已保存，备份文件待重试"
    : "";
}

export function snapshotWarningAfterWrite(
  current: string,
  snapshot?: Pick<SnapshotSummary, "mirrorState">
): string {
  return snapshotMirrorWarning(snapshot) || current;
}

const errorTranslations: Array<[RegExp, string]> = [
  [/AMAP_WEBSERVICE_KEY is not configured/i, "天气服务尚未配置"],
  [/AMap JSAPI proxy is not configured/i, "地图服务尚未配置"],
  [/AMap service could not be reached/i, "地图服务暂时无法连接"],
  [/weather could not be refreshed/i, "天气暂时无法刷新"],
  [/invalid mobile or password/i, "手机号或密码错误"],
  [/sign in (first|again)/i, "请重新登录"],
  [/resource was changed by another user/i, "数据已在其他位置更新，请刷新后重试"],
  [/day date already exists in this trip/i, "当前旅行已经存在该日期"],
  [/day date must be within the trip date range/i, "执行日必须位于旅行日期范围内"],
  [/trip date range would exclude existing day/i, "日期范围不能排除已有执行日，请先调整对应日程"],
  [/trip date range cannot exceed 366 days/i, "旅行日期范围不能超过 366 天"],
  [/resource was not found/i, "未找到对应数据"],
  [/endpoint not found/i, "未找到对应功能"],
  [/database (schema )?is (unavailable|not initialized)/i, "数据服务暂时不可用"],
  [/request could not be completed/i, "操作未能完成，请稍后重试"]
];

export function localizeAPIError(detail: string, status?: number): string {
  const trimmed = detail.trim();
  const matched = errorTranslations.find(([pattern]) => pattern.test(trimmed));
  if (matched) return matched[1];
  if (/[\u3400-\u9fff]/u.test(trimmed)) return trimmed;
  return status ? `操作失败，服务响应状态码为 ${status}` : "操作失败，请稍后重试";
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");
  if (options.body) headers.set("Content-Type", "application/json");
  if (!["GET", "HEAD"].includes(options.method ?? "GET")) headers.set("X-CSRF-Token", csrfToken);
  const response = await fetch(`${apiBase}${path}`, { ...options, headers, credentials: "include" });
  if (response.status === 204) return undefined as T;
  const contentType = response.headers.get("content-type") ?? "";
  const payload = /(?:^|[+/])json(?:\s*;|$)/i.test(contentType)
    ? await response.json() as T & { detail?: string; requestId?: string }
    : undefined;
  if (!response.ok) {
    const requestId = payload?.requestId ? `（请求编号 ${payload.requestId}）` : "";
    throw new Error(`${localizeAPIError(payload?.detail ?? "", response.status)}${requestId}`);
  }
  if (payload === undefined) throw new Error("服务返回了无法识别的响应");
  return payload;
}

async function download(path: string): Promise<{ blob: Blob; filename: string }> {
  const response = await fetch(`${apiBase}${path}`, {
    headers: { Accept: "application/json" },
    credentials: "include"
  });
  if (!response.ok) {
    const contentType = response.headers.get("content-type") ?? "";
    const payload = /(?:^|[+/])json(?:\s*;|$)/i.test(contentType)
      ? await response.json() as { detail?: string; requestId?: string }
      : undefined;
    const requestId = payload?.requestId ? `（请求编号 ${payload.requestId}）` : "";
    throw new Error(`${localizeAPIError(payload?.detail ?? "", response.status)}${requestId}`);
  }
  const disposition = response.headers.get("content-disposition") ?? "";
  const match = disposition.match(/filename="([^"]+)"/i);
  return {
    blob: await response.blob(),
    filename: match?.[1] ?? "trip-backup.json"
  };
}

export const api = {
  login: (mobile: string, password: string) => request<Session>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ mobile, password })
  }),
  logout: () => request<void>("/auth/logout", { method: "POST" }),
  session: () => request<Session>("/session"),
  profile: () => request<{ record: LibraryRecord; user: Session["user"] }>("/profile"),
  updateProfile: (
    row: LibraryRecord,
    input: {
      displayName: string;
      mobile: string;
      alternateContact: string;
      emergencyContact: string;
      currentPassword: string;
      newPassword: string;
    }
  ) => request<{
    record: LibraryRecord;
    user: Session["user"];
    snapshot: SnapshotSummary;
  }>("/profile", {
    method: "PATCH",
    headers: { "If-Match": String(row.version) },
    body: JSON.stringify(input)
  }),
  library: () => request<{ library: LibraryState }>("/library"),
  libraryRecords: async (resource: LibraryResource) => {
    const payload = await request<{ records: LibraryRecord[] | null }>(`/library/${resource}`);
    return { records: arrayOrEmpty(payload.records) };
  },
  createLibraryRecord: (
    resource: LibraryResource,
    recordKey: string,
    data: Record<string, unknown>,
    password?: string
  ) => request<{
    record: LibraryRecord;
    snapshot: SnapshotSummary;
    replayed: boolean;
  }>(`/library/${resource}`, {
    method: "POST",
    headers: { "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify({ recordKey, data, password })
  }),
  addTravelTypeTagPresets: () => request<{
    records: LibraryRecord[];
    snapshot?: SnapshotSummary;
    replayed: boolean;
  }>("/library/tags/presets", {
    method: "POST",
    headers: { "Idempotency-Key": crypto.randomUUID() }
  }),
  updateLibraryRecord: (
    resource: LibraryResource,
    row: LibraryRecord,
    data: Record<string, unknown>,
    password?: string
  ) => request<{
    record: LibraryRecord;
    snapshot: SnapshotSummary;
  }>(`/library/${resource}/${row.id}`, {
    method: "PATCH",
    headers: { "If-Match": String(row.version) },
    body: JSON.stringify({ data, password })
  }),
  deleteLibraryRecord: (
    resource: LibraryResource,
    row: LibraryRecord,
    options: {
      replaceTagWithOther?: boolean;
      deleteLoginCredential?: boolean;
    } = {}
  ) => request<{ deleted: boolean; snapshot: SnapshotSummary }>(
    `/library/${resource}/${row.id}${queryString({
      replaceTagWithOther: options.replaceTagWithOther,
      deleteLoginCredential: options.deleteLoginCredential
    })}`,
    {
      method: "DELETE",
      headers: { "If-Match": String(row.version) }
    }
  ),
  libraryDeletionImpact: (resource: LibraryResource, row: LibraryRecord) => (
    request<LibraryDeletionImpact>(
      `/library/${resource}/${row.id}/deletion-impact`
    )
  ),
  trips: async () => {
    const payload = await request<{ trips: Trip[] | null }>("/trips");
    return { trips: arrayOrEmpty(payload.trips) };
  },
  createTrip: (trip: CreateTripInput) => request<{
    trip: Trip;
    snapshot: SnapshotSummary;
    replayed: boolean;
  }>("/trips", {
    method: "POST",
    headers: { "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify(trip)
  }),
  updateTrip: (
    trip: Trip,
    patch: UpdateTripInput
  ) => request<{
    trip: Trip;
    snapshot: SnapshotSummary;
  }>(`/trips/${trip.id}`, {
    method: "PATCH",
    headers: { "If-Match": String(trip.version) },
    body: JSON.stringify(patch)
  }),
  deleteTrip: (trip: Trip) => request<{
    deleted: boolean;
    snapshot: SnapshotSummary;
  }>(`/trips/${trip.id}`, {
    method: "DELETE",
      headers: { "If-Match": String(trip.version) }
    }),
  downloadTripBackup: (tripId: string) => download(
    `/trips/${encodeURIComponent(tripId)}/backup`
  ),
  previewTripBackupImport: (backup: unknown) => request<TripBackupImportPreview>(
    "/trip-imports/preview",
    {
      method: "POST",
      body: JSON.stringify({ backup })
    }
  ),
  applyTripBackupImport: (
    backup: unknown,
    confirmChecksum: string,
    memberMappings: TripBackupDependencyMapping[],
    tagMappings: TripBackupDependencyMapping[]
  ) => request<{
    trip: Trip;
    snapshot: SnapshotSummary;
    librarySnapshot?: SnapshotSummary;
    replayed: boolean;
  }>("/trip-imports/apply", {
    method: "POST",
    headers: { "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify({
      backup,
      confirmChecksum,
      memberMappings,
      tagMappings
    })
  }),
  overview: (tripId: string) => request<{ counts: Record<string, number> }>(`/trips/${tripId}/overview`),
  weather: (tripId: string, adcode: string, refresh = false) => request<WeatherResponse>(`/trips/${tripId}/weather?adcode=${encodeURIComponent(adcode)}${refresh ? "&refresh=true" : ""}`),
  mapConfig: (tripId: string) => request<MapConfig>(`/trips/${tripId}/map-config`),
  routePlan: (
    tripId: string,
    mode: "drive" | "taxi" | "rideshare" | "walk" | "bicycle",
    origin: [number, number],
    destination: [number, number]
  ) => request<RoutePlan>(
    `/trips/${tripId}/route?${new URLSearchParams({
      mode,
      origin: origin.join(","),
      destination: destination.join(",")
    }).toString()}`
  ),
  placeSearch: async (tripId: string, keywords: string) => {
    const payload = await request<{ candidates: PlaceCandidate[] | null }>(
      `/trips/${tripId}/place-search?keywords=${encodeURIComponent(keywords)}`
    );
    return { candidates: arrayOrEmpty(payload.candidates) };
  },
  reverseGeocode: (
    tripId: string,
    longitude: number,
    latitude: number
  ) => request<{ address: string; adcode?: string; region?: string }>(
    `/trips/${tripId}/reverse-geocode?location=${encodeURIComponent(
      `${longitude},${latitude}`
    )}`
  ),
  records: async (tripId: string, resource: Resource) => {
    const payload = await request<{ records: RecordRow[] | null }>(`/trips/${tripId}/${resource}`);
    return { records: arrayOrEmpty(payload.records) };
  },
  createRecord: (
    tripId: string,
    resource: Resource,
    recordKey: string,
    data: Record<string, unknown>,
    idempotencyKey: string
  ) => request<{
    record: RecordRow;
    snapshot: SnapshotSummary;
    replayed: boolean;
  }>(`/trips/${tripId}/${resource}`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ recordKey, data })
  }),
  createPlaceForDay: (
    tripId: string,
    day: RecordRow,
    recordKey: string,
    data: Record<string, unknown>,
    idempotencyKey: string
  ) => request<{
    place: RecordRow;
    day: RecordRow;
    snapshot: SnapshotSummary;
    replayed: boolean;
  }>(`/trips/${tripId}/days/${day.id}/places`, {
    method: "POST",
    headers: {
      "If-Match": String(day.version),
      "Idempotency-Key": idempotencyKey
    },
    body: JSON.stringify({ recordKey, data })
  }),
  auditLogs: async (tripId: string) => {
    const payload = await request<{ records: AuditLog[] | null }>(`/trips/${tripId}/audit-logs`);
    return { records: arrayOrEmpty(payload.records) };
  },
  reorderRecords: (
    tripId: string,
    resource: "segments",
    groupKey: string,
    ids: string[],
    baseRevision: number,
    idempotencyKey: string = crypto.randomUUID()
  ) => request<{ snapshot: SnapshotSummary; replayed: boolean }>(
    `/trips/${tripId}/${resource}/reorder`,
    {
      method: "POST",
      headers: {
        "If-Match": String(baseRevision),
        "Idempotency-Key": idempotencyKey
      },
      body: JSON.stringify({ groupKey, ids })
    }
  ),
  updateRecord: (
    tripId: string,
    resource: Resource,
    row: RecordRow,
    data: Record<string, unknown>
  ) => request<{
    record: RecordRow;
    snapshot: SnapshotSummary;
  }>(`/trips/${tripId}/${resource}/${row.id}`, {
    method: "PATCH",
    headers: { "If-Match": String(row.version) },
    body: JSON.stringify({ data })
  }),
  deleteRecord: (
    tripId: string,
    resource: Resource,
    row: RecordRow,
    cascade = false
  ) => request<{
    deleted: boolean;
    snapshot: SnapshotSummary;
  }>(`/trips/${tripId}/${resource}/${row.id}${cascade ? "?cascade=true" : ""}`, {
    method: "DELETE",
    headers: { "If-Match": String(row.version) }
  }),
  tripMemberDeletionImpact: (
    tripId: string,
    row: RecordRow
  ) => request<TripMemberDeletionImpact>(
    `/trips/${tripId}/members/${row.id}/deletion-impact`
  )
};

export function mapServiceHost(origin = window.location.origin): string {
  return new URL("/_AMapService", origin).toString();
}

function queryString(values: Record<string, boolean | undefined>): string {
  const query = new URLSearchParams();
  for (const [key, enabled] of Object.entries(values)) {
    if (enabled) query.set(key, "true");
  }
  const encoded = query.toString();
  return encoded ? `?${encoded}` : "";
}
