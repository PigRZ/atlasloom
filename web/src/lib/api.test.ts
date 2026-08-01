// API 工具测试固定空集合归一化，避免后端 null 泄漏到视图。
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  api,
  arrayOrEmpty,
  localizeAPIError,
  mapServiceHost,
  setCSRFToken,
  snapshotMirrorWarning,
  snapshotWarningAfterWrite
} from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("arrayOrEmpty", () => {
  it("normalizes empty Mongo collection payloads", () => {
    expect(arrayOrEmpty(null)).toEqual([]);
    expect(arrayOrEmpty(undefined)).toEqual([]);
  });

  it("preserves populated arrays", () => {
    const rows = [{ id: "record-1" }];
    expect(arrayOrEmpty(rows)).toBe(rows);
  });
});

describe("snapshot mirror feedback", () => {
  it("warns only when the committed snapshot mirror is pending", () => {
    expect(snapshotMirrorWarning({ mirrorState: "pending" }))
      .toBe("数据已保存，备份文件待重试");
    expect(snapshotMirrorWarning({ mirrorState: "mirrored" })).toBe("");
    expect(snapshotMirrorWarning()).toBe("");
  });

  it("keeps an existing warning after a later mirrored write", () => {
    const warning = "数据已保存，备份文件待重试";
    expect(snapshotWarningAfterWrite(warning, { mirrorState: "mirrored" }))
      .toBe(warning);
    expect(snapshotWarningAfterWrite("", { mirrorState: "pending" }))
      .toBe(warning);
  });
});

describe("trip mutations", () => {
  it("updates only the current editable Trip fields, including the summary", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      trip: { id: "trip-a", version: 4 },
      snapshot: { revision: 4, mirrorState: "mirrored" }
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.updateTrip({
      id: "trip-a",
      title: "原旅行",
      summary: "原说明",
      startDate: "2026-09-25",
      endDate: "2026-09-28",
      timezone: "Asia/Shanghai",
      currency: "CNY",
      version: 3,
      role: "owner"
    }, {
      title: "新旅行",
      summary: "新的旅行说明",
      startDate: "2026-09-26",
      endDate: "2026-09-29"
    });

    const [url, options] = fetchMock.mock.calls[0]!;
    expect(String(url)).toContain("/api/trips/trip-a");
    expect(new Headers(options.headers).get("If-Match")).toBe("3");
    expect(JSON.parse(String(options.body))).toEqual({
      title: "新旅行",
      summary: "新的旅行说明",
      startDate: "2026-09-26",
      endDate: "2026-09-29"
    });
  });
});

describe("record creation", () => {
  it("keeps the request idempotency key separate from the business record key", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      record: { id: "day-a" },
      snapshot: { revision: 1, mirrorState: "mirrored" },
      replayed: false
    }), {
      status: 201,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.createRecord(
      "trip-a",
      "days",
      "d1",
      { title: "第一天", date: "2026-10-01" },
      "create-day-request-a"
    );

    const [, options] = fetchMock.mock.calls[0]!;
    expect(new Headers(options.headers).get("Idempotency-Key"))
      .toBe("create-day-request-a");
    expect(JSON.parse(String(options.body))).toMatchObject({ recordKey: "d1" });
  });

  it("creates and attaches a place with the Day version in one request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      place: { id: "place-a" },
      day: { id: "day-a", version: 3 },
      snapshot: { revision: 3, mirrorState: "mirrored" },
      replayed: false
    }), {
      status: 201,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.createPlaceForDay(
      "trip-a",
      {
        id: "day-a",
        tripId: "trip-a",
        resource: "days",
        recordKey: "day-a",
        position: 0,
        version: 2,
        data: { title: "第一天", date: "2026-10-01" },
        createdByMemberId: "member-a",
        updatedByMemberId: "member-a",
        createdAt: "",
        updatedAt: ""
      },
      "place-a",
      { name: "示例东站", type: "tag-place" },
      "create-day-place-request-a"
    );

    const [url, options] = fetchMock.mock.calls[0]!;
    const headers = new Headers(options.headers);
    expect(String(url)).toContain("/api/trips/trip-a/days/day-a/places");
    expect(headers.get("If-Match")).toBe("2");
    expect(headers.get("Idempotency-Key")).toBe("create-day-place-request-a");
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token");
  });
});

describe("global library", () => {
  it("loads the records affected by deleting a base-library item", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      references: [{
        tripId: "trip-a",
        tripTitle: "测试旅行",
        tripDeleted: false,
        resource: "places",
        recordId: "place-a",
        label: "测试地点",
        relations: ["place-type"]
      }],
      hasLoginCredential: false,
      hasTripOwnership: false,
      canDeleteWithCredential: false,
      canReplaceWithFallback: true
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);

    const row = {
      id: "tag-a",
      resource: "tags" as const,
      recordKey: "tag-a",
      position: 0,
      version: 1,
      data: { name: "测试标签" },
      createdAt: "",
      updatedAt: ""
    };
    await expect(api.libraryDeletionImpact("tags", row)).resolves.toMatchObject({
      references: [{ recordId: "place-a" }]
    });
    expect(String(fetchMock.mock.calls[0]![0]))
      .toContain("/api/library/tags/tag-a/deletion-impact");
  });

  it("requires an explicit query flag before revoking a member login", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      deleted: true,
      snapshot: { revision: 3, mirrorState: "mirrored" }
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    const row = {
      id: "member-a",
      resource: "members" as const,
      recordKey: "member-a",
      position: 0,
      version: 2,
      data: { displayName: "待删除成员", status: "inactive" },
      createdAt: "",
      updatedAt: ""
    };
    await api.deleteLibraryRecord("members", row, {
      deleteLoginCredential: true
    });

    const [url, options] = fetchMock.mock.calls[0]!;
    expect(String(url)).toContain(
      "/api/library/members/member-a?deleteLoginCredential=true"
    );
    expect(new Headers(options.headers).get("If-Match")).toBe("2");
  });

  it("loads privacy-safe dependencies before deleting a trip member", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      references: [{
        resource: "expenses",
        recordId: "expense-a",
        label: "晚餐",
        relations: ["expense-payer", "expense-split"]
      }],
      cascadeExpenses: [],
      hasPrivatePackingItems: true
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);

    const row = {
      id: "trip-member-a",
      tripId: "trip-a",
      resource: "members" as const,
      recordKey: "trip-member-a",
      position: 0,
      version: 1,
      data: { memberId: "member-a" },
      createdByMemberId: "member-a",
      updatedByMemberId: "member-a",
      createdAt: "",
      updatedAt: ""
    };
    await expect(api.tripMemberDeletionImpact("trip-a", row)).resolves.toMatchObject({
      references: [{ label: "晚餐" }],
      cascadeExpenses: [],
      hasPrivatePackingItems: true
    });
    expect(String(fetchMock.mock.calls[0]![0]))
      .toContain("/api/trips/trip-a/members/trip-member-a/deletion-impact");
  });

  it("creates a member outside trip routes and keeps the password in the JSON body", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      record: { id: "member-a" },
      snapshot: { revision: 2, mirrorState: "mirrored" },
      replayed: false
    }), {
      status: 201,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.createLibraryRecord(
      "members",
      "member-a",
      { displayName: "测试成员", mobile: "13800138000", status: "active" },
      "secret-password"
    );

    const [url, options] = fetchMock.mock.calls[0]!;
    expect(String(url)).toContain("/api/library/members");
    expect(String(url)).not.toContain("/trips/");
    expect(new Headers(options.headers).get("X-CSRF-Token")).toBe("csrf-token");
    expect(JSON.parse(String(options.body))).toMatchObject({
      data: { displayName: "测试成员", mobile: "13800138000" },
      password: "secret-password"
    });
  });
});

describe("own profile", () => {
  it("updates the current member with revision and CSRF preconditions", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      record: { id: "member-a", version: 3 },
      user: {
        id: "member-a",
        displayName: "新姓名",
        mobile: "13900139000",
        admin: false
      },
      snapshot: { revision: 3, mirrorState: "mirrored" }
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.updateProfile({
      id: "member-a",
      resource: "members",
      recordKey: "member-a",
      position: 0,
      version: 2,
      data: { displayName: "旧姓名", mobile: "13800138000" },
      createdAt: "",
      updatedAt: ""
    }, {
      displayName: "新姓名",
      mobile: "13900139000",
      alternateContact: "微信同号",
      emergencyContact: "家人 13700137000",
      currentPassword: "current-password",
      newPassword: "new-password"
    });

    const [url, options] = fetchMock.mock.calls[0]!;
    const headers = new Headers(options.headers);
    expect(String(url)).toContain("/api/profile");
    expect(options.method).toBe("PATCH");
    expect(headers.get("If-Match")).toBe("2");
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token");
    expect(JSON.parse(String(options.body))).toMatchObject({
      mobile: "13900139000",
      currentPassword: "current-password",
      newPassword: "new-password"
    });
  });
});

describe("place lookup", () => {
  it("uses the fixed first-level AMap security proxy route", () => {
    expect(mapServiceHost("https://travel.example.test"))
      .toBe("https://travel.example.test/_AMapService");
  });

  it("searches through the authenticated server endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      candidates: [{
        id: "poi-1",
        name: "示例东站",
        address: "示例省示例市示例区",
        longitude: 116.397428,
        latitude: 39.90923
      }]
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);

    const result = await api.placeSearch("trip-a", "示例东站");

    expect(result.candidates[0]?.name).toBe("示例东站");
    const url = String(fetchMock.mock.calls[0]![0]);
    expect(url).toContain("/api/trips/trip-a/place-search?");
    expect(url).toContain(`keywords=${encodeURIComponent("示例东站")}`);
  });
});

describe("record ordering", () => {
  it("sends the complete day order with trip revision and idempotency headers", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      snapshot: { revision: 8 },
      replayed: false
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.reorderRecords(
      "trip-a",
      "segments",
      "day-a",
      ["segment-b", "segment-a"],
      7,
      "reorder-request-a"
    );

    const [url, options] = fetchMock.mock.calls[0]!;
    const headers = new Headers(options.headers);
    expect(String(url)).toContain("/api/trips/trip-a/segments/reorder");
    expect(headers.get("If-Match")).toBe("7");
    expect(headers.get("Idempotency-Key")).toBe("reorder-request-a");
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token");
    expect(JSON.parse(String(options.body))).toEqual({
      groupKey: "day-a",
      ids: ["segment-b", "segment-a"]
    });
  });
});

describe("record deletion", () => {
  it("uses the record version as the delete precondition", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      deleted: true,
      snapshot: { revision: 9 }
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.deleteRecord("trip-a", "segments", {
      id: "segment-a",
      tripId: "trip-a",
      resource: "segments",
      recordKey: "segment-a",
      position: 0,
      version: 4,
      data: { dayId: "day-a", fromPlaceId: "place-a", toPlaceId: "place-b", mode: "drive" },
      createdByMemberId: "member-a",
      updatedByMemberId: "member-a",
      createdAt: "2026-07-26T00:00:00Z",
      updatedAt: "2026-07-26T00:00:00Z"
    });

    const [url, options] = fetchMock.mock.calls[0]!;
    const headers = new Headers(options.headers);
    expect(String(url)).toContain("/api/trips/trip-a/segments/segment-a");
    expect(options.method).toBe("DELETE");
    expect(headers.get("If-Match")).toBe("4");
    expect(headers.get("X-CSRF-Token")).toBe("csrf-token");
  });

  it("sends explicit cascade confirmation when deleting a place", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      deleted: true,
      snapshot: { revision: 10 }
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.deleteRecord("trip-a", "places", {
      id: "place-a",
      tripId: "trip-a",
      resource: "places",
      recordKey: "place-a",
      position: 0,
      version: 3,
      data: { name: "测试地点" },
      createdByMemberId: "member-a",
      updatedByMemberId: "member-a",
      createdAt: "2026-07-26T00:00:00Z",
      updatedAt: "2026-07-26T00:00:00Z"
    }, true);

    expect(String(fetchMock.mock.calls[0]![0]))
      .toContain("/api/trips/trip-a/places/place-a?cascade=true");
  });

  it("sends explicit cascade confirmation when deleting a packing-only member", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      deleted: true,
      snapshot: { revision: 11 }
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);
    setCSRFToken("csrf-token");

    await api.deleteRecord("trip-a", "members", {
      id: "trip-member-a",
      tripId: "trip-a",
      resource: "members",
      recordKey: "trip-member-a",
      position: 0,
      version: 2,
      data: { memberId: "member-a" },
      createdByMemberId: "member-a",
      updatedByMemberId: "member-a",
      createdAt: "2026-07-26T00:00:00Z",
      updatedAt: "2026-07-26T00:00:00Z"
    }, true);

    expect(String(fetchMock.mock.calls[0]![0]))
      .toContain("/api/trips/trip-a/members/trip-member-a?cascade=true");
  });
});

describe("problem details", () => {
  it("translates detail and preserves the request id from problem details", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      detail: "resource was changed by another user",
      requestId: "request-1234"
    }), {
      status: 412,
      headers: { "Content-Type": "application/problem+json; charset=utf-8" }
    })));

    await expect(api.session()).rejects.toThrow(
      "数据已在其他位置更新，请刷新后重试（请求编号 request-1234）"
    );
  });

  it("never exposes an unknown English detail to the interface", () => {
    expect(localizeAPIError("unknown upstream failure", 502))
      .toBe("操作失败，服务响应状态码为 502");
    expect(localizeAPIError("当前日期不可用", 400)).toBe("当前日期不可用");
  });
});
