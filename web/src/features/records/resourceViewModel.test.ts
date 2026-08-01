import { describe, expect, it } from "vitest";
import type { DayData, RecordRow, Resource } from "../../lib/types";
import {
  expenseRecordState,
  filterPackingRecords,
  filterResearchRecords,
  filterResourceRecords,
  resourceCreationBlocker
} from "./resourceViewModel";

const rows = [
  research("a", { dayIds: ["day-a"], placeId: "place-a" }),
  research("b", { dayIds: ["day-a", "day-b"], placeId: "place-b" }),
  research("c", { dayIds: ["day-b"] })
];

describe("filterResearchRecords", () => {
  it("按执行日筛选", () => {
    expect(filterResearchRecords(rows, "day-a", "").map((row) => row.id))
      .toEqual(["a", "b"]);
  });

  it("按地点筛选，并在两个条件同时存在时取交集", () => {
    expect(filterResearchRecords(rows, "", "place-b").map((row) => row.id))
      .toEqual(["b"]);
    expect(filterResearchRecords(rows, "day-b", "place-b").map((row) => row.id))
      .toEqual(["b"]);
    expect(filterResearchRecords(rows, "day-a", "place-a").map((row) => row.id))
      .toEqual(["a"]);
  });
});

describe("filterPackingRecords", () => {
  const packing = [
    record("packing-a", "packing-items", { item: "雨衣", dayIds: ["day-a"] }),
    record("packing-b", "packing-items", { item: "充电宝", dayIds: ["day-b"] }),
    record("packing-c", "packing-items", { item: "证件" })
  ];

  it("按 dayIds 筛选当天物品，未选日期时保留全部", () => {
    expect(filterPackingRecords(packing, "day-a").map((row) => row.id))
      .toEqual(["packing-a"]);
    expect(filterPackingRecords(packing, "")).toEqual(packing);
  });
});

describe("filterResourceRecords", () => {
  const places = [
    record("place-a", "places", { name: "甲", type: "scenic" }),
    record("place-b", "places", { name: "乙", type: "hotel" })
  ];
  const days = [
    record("day-a", "days", {
      title: "第一天",
      date: "2026-07-28",
      placeEntries: [{ placeId: "place-a" }]
    }) as RecordRow<DayData>
  ];

  it("地点类型与关联日期取交集", () => {
    expect(filterResourceRecords(
      places,
      "places",
      { typeID: "scenic", dayID: "day-a" },
      days
    ).map((row) => row.id)).toEqual(["place-a"]);
    expect(filterResourceRecords(
      places,
      "places",
      { typeID: "hotel", dayID: "day-a" },
      days
    )).toEqual([]);
  });

  it("按预订类型和预订人筛选，并按费用类型筛选", () => {
    const bookings = [
      record("booking-a", "bookings", {
        item: "甲", type: "hotel", bookerMemberId: "member-a"
      }),
      record("booking-b", "bookings", {
        item: "乙", type: "ticket", bookerMemberId: "member-b"
      })
    ];
    expect(filterResourceRecords(
      bookings,
      "bookings",
      { typeID: "ticket", memberID: "member-b" }
    ).map((row) => row.id)).toEqual(["booking-b"]);
    expect(filterResourceRecords(
      [record("expense-a", "expenses", {
        item: "甲",
        type: "ticket",
        amountMinor: 3200,
        payerMemberId: "member-a"
      })],
      "expenses",
      { typeID: "ticket" }
    ).map((row) => row.id)).toEqual(["expense-a"]);
  });

  it("费用登记状态由金额和支付人推导并可与类型筛选组合", () => {
    const expenses = [
      record("expense-a", "expenses", {
        item: "酒店",
        type: "hotel",
        amountMinor: 88000,
        payerMemberId: "member-a"
      }),
      record("expense-b", "expenses", {
        item: "门票",
        type: "ticket",
        payerMemberId: "member-a"
      })
    ];
    expect(expenseRecordState(expenses[0]!)).toBe("ready");
    expect(expenseRecordState(expenses[1]!)).toBe("pending");
    expect(filterResourceRecords(
      expenses,
      "expenses",
      { expenseState: "pending" }
    ).map((row) => row.id)).toEqual(["expense-b"]);
    expect(filterResourceRecords(
      expenses,
      "expenses",
      { typeID: "hotel", expenseState: "ready" }
    ).map((row) => row.id)).toEqual(["expense-a"]);
  });
});

describe("resourceCreationBlocker", () => {
  const ready = {
    tripId: "trip-a",
    placeTypeCount: 1,
    expenseTypeCount: 1,
    platformTypeCount: 1,
    selectedMemberCount: 1,
    availableMemberCount: 1
  };

  it("guides users to the exact missing dependency", () => {
    expect(resourceCreationBlocker("places", {
      ...ready,
      placeTypeCount: 0
    })).toMatchObject({
      actionLabel: "维护地点类型",
      href: "/app/library?section=tags"
    });
    expect(resourceCreationBlocker("expenses", {
      ...ready,
      selectedMemberCount: 0
    })).toMatchObject({
      actionLabel: "选择旅行成员",
      href: "/app/trips/trip-a/members"
    });
    expect(resourceCreationBlocker("bookings", {
      ...ready,
      expenseTypeCount: 0
    })).toMatchObject({
      actionLabel: "维护费用类型"
    });
  });

  it("allows creation when every required dependency exists", () => {
    expect(resourceCreationBlocker("places", ready)).toBeNull();
    expect(resourceCreationBlocker("bookings", ready)).toBeNull();
    expect(resourceCreationBlocker("expenses", ready)).toBeNull();
    expect(resourceCreationBlocker("research-evidence", ready)).toBeNull();
  });
});

function research(id: string, data: Record<string, unknown>): RecordRow {
  return {
    id,
    tripId: "trip-a",
    recordKey: `research-${id}`,
    resource: "research-evidence",
    position: 0,
    version: 1,
    data: { title: id, ...data },
    createdByMemberId: "member-a",
    updatedByMemberId: "member-a",
    createdAt: "2026-07-28T00:00:00Z",
    updatedAt: "2026-07-28T00:00:00Z"
  };
}

function record(
  id: string,
  resource: Resource,
  data: Record<string, unknown>
): RecordRow {
  return {
    id,
    tripId: "trip-a",
    recordKey: `${resource}-${id}`,
    resource,
    position: 0,
    version: 1,
    data,
    createdByMemberId: "member-a",
    updatedByMemberId: "member-a",
    createdAt: "2026-07-28T00:00:00Z",
    updatedAt: "2026-07-28T00:00:00Z"
  };
}
