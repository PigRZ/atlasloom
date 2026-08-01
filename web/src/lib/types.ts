// 共享类型镜像公开 API 结构，不包含具体旅行实例数据。
export type Resource =
  | "days"
  | "places"
  | "segments"
  | "bookings"
  | "expenses"
  | "packing-items"
  | "research-evidence"
  | "members";

export type PageResource = Exclude<Resource, "days" | "segments">;

export type LibraryResource = "members" | "tags";

export type TransportMode =
  | "walk"
  | "drive"
  | "taxi"
  | "rideshare"
  | "public_transit"
  | "train"
  | "flight"
  | "shuttle"
  | "scenic_bus"
  | "ferry"
  | "bicycle"
  | "other";

export type Trip = {
  id: string;
  title: string;
  summary: string;
  startDate: string;
  endDate: string;
  timezone: string;
  currency: string;
  version: number;
  role?: "owner" | "editor" | "viewer";
};

export type CreateTripInput = Pick<
  Trip,
  "id" | "title" | "startDate" | "endDate"
> & Partial<Pick<Trip, "summary" | "timezone" | "currency">>;

export type UpdateTripInput = Partial<Pick<
  Trip,
  "title" | "summary" | "startDate" | "endDate" | "timezone" | "currency"
>>;

export type RecordRow<T extends Record<string, unknown> = Record<string, unknown>> = {
  id: string;
  tripId: string;
  recordKey: string;
  resource: Resource;
  position: number;
  version: number;
  data: T;
  createdByMemberId: string;
  updatedByMemberId: string;
  createdAt: string;
  updatedAt: string;
};

export type DayData = Record<string, unknown> & {
  title: string;
  date: string;
  placeEntries?: DayPlaceEntry[];
  additionalInfo?: string;
};

export type DayPlaceEntry = {
  placeId: string;
  arrivalTime?: string;
  departureTime?: string;
  additionalInfo?: string;
};

export type PlaceData = Record<string, unknown> & {
  name: string;
  type?: string;
  address?: string;
  region?: string;
  latitude?: number;
  longitude?: number;
  weatherAdcode?: string;
  navigationURL?: string;
  openingHours?: string;
  recommendedTicketPlatform?: string;
};

export type SegmentData = Record<string, unknown> & {
  dayId: string;
  fromPlaceId: string;
  toPlaceId: string;
  mode: TransportMode;
  durationMinutes?: number;
  distanceMeters?: number;
  departureTime?: string;
  navigationURL?: string;
  additionalInfo?: string;
};

export type Session = {
  user: { id: string; displayName: string; mobile: string; admin: boolean };
  csrfToken: string;
  expiresAt: string;
};

export type WeatherResponse = {
  city: string;
  adcode: string;
  province: string;
  reportedAt: string;
  fetchedAt: string;
  expiresAt: string;
  freshness: "fresh" | "stale";
  cached: boolean;
  refreshError?: string;
  forecasts: Array<{
    date: string;
    dayWeather: string;
    nightWeather: string;
    dayTemp: string;
    nightTemp: string;
    dayWind: string;
    nightWind: string;
    dayPower: string;
    nightPower: string;
  }>;
};

export type MapConfig = {
  enabled: boolean;
  jsapiKey: string;
};

export type RoutePlan = {
  distanceMeters: number;
  durationSeconds: number;
  path: Array<[number, number]>;
};

export type PlaceCandidate = {
  id: string;
  name: string;
  address: string;
  longitude: number;
  latitude: number;
  adcode?: string;
  region?: string;
};

export type SnapshotSummary = {
  id: string;
  revision: number;
  sha256: string;
  contentChecksum: string;
  mirrorState: "pending" | "mirrored";
  createdAt: string;
};

export type TripBackupDependencyPreview = {
  sourceId: string;
  displayName?: string;
  name?: string;
  category?: "general" | "place-type" | "expense-type" | "platform-type";
  status: "matched-id" | "matched-semantic" | "missing" | "conflict";
  targetId?: string;
};

export type TripBackupImportPreview = {
  valid: boolean;
  ready: boolean;
  validationErrors: string[];
  checksum?: string;
  suggestedTripId?: string;
  trip: Pick<Trip, "title" | "startDate" | "endDate">;
  counts: Record<string, number>;
  members: TripBackupDependencyPreview[];
  tags: TripBackupDependencyPreview[];
};

export type TripBackupDependencyMapping = {
  sourceId: string;
  targetId?: string;
  create: boolean;
};

export type LibraryState = {
  id: string;
  version: number;
  updatedAt: string;
};

export type LibraryRecord = {
  id: string;
  resource: LibraryResource;
  recordKey: string;
  position: number;
  version: number;
  data: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type LibraryReference = {
  tripId: string;
  tripTitle: string;
  tripDeleted: boolean;
  resource: Resource;
  recordId?: string;
  label: string;
  relations: Array<
    | "trip-member"
    | "booking-booker"
    | "research-recorder"
    | "expense-payer"
    | "expense-split"
    | "packing-owner"
    | "record-creator"
    | "record-updater"
    | "place-type"
    | "booking-type"
    | "expense-type"
    | "booking-platform"
    | "research-platform"
  >;
};

export type LibraryDeletionImpact = {
  references: LibraryReference[];
  hasLoginCredential: boolean;
  hasTripOwnership: boolean;
  canDeleteWithCredential: boolean;
  canReplaceWithFallback: boolean;
};

export type TripMemberReference = {
  resource: "bookings" | "expenses" | "research-evidence";
  recordId: string;
  label: string;
  relations: Array<
    "booking-booker" | "expense-payer" | "expense-split" | "research-recorder"
  >;
};

export type TripMemberDeletionImpact = {
  references: TripMemberReference[];
  cascadeExpenses: TripMemberReference[];
  hasPrivatePackingItems: boolean;
};

export type AuditLog = {
  id: string;
  tripId?: string;
  memberId: string;
  module: string;
  action: string;
  resource?: string;
  recordId?: string;
  recordLabel?: string;
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  createdAt: string;
};
