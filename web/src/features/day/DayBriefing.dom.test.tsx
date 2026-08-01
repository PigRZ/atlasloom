// @vitest-environment jsdom
// 当日摘要测试锁定地点优先、时间可选和不落库的准备概况。
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { RecordRow, Resource } from "../../lib/types";
import { DayBriefing } from "./DayBriefing";
import type { DayPlan } from "./dayModel";

describe("当日行动摘要", () => {
  it("把地点作为最低维护单元，并把交通和时间明确为可选", () => {
    const place = record("place-a", "places", { name: "示例游客中心" });
    const day = record("day-a", "days", {
      title: "抵达日",
      date: "2026-09-25",
      placeEntries: [{ placeId: place.id }]
    });
    const plan: DayPlan = {
      day,
      placesById: new Map([[place.id, place]]),
      entries: [{
        id: "day-a:0:place-a",
        entry: { placeId: place.id },
        place,
        order: 1
      }],
      segments: []
    };

    render(
      <DayBriefing
        plan={plan}
        bookings={[record("booking-a", "bookings", {
          item: "门票",
          status: "needs_booking"
        })]}
        packing={[
          record("packing-a", "packing-items", { item: "雨衣", packed: true }),
          record("packing-b", "packing-items", { item: "充电宝", packed: false })
        ]}
      />
    );

    expect(screen.getByText("地点优先")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "当天有 1 个想去的地点" })).toBeInTheDocument();
    expect(screen.getByText("顺序只是参考，时间和交通都可以不填，到了现场再决定怎么走。"))
      .toBeInTheDocument();
    expect(screen.getByText("可不填")).toBeInTheDocument();
    expect(screen.getByText("1 待处理")).toBeInTheDocument();
    expect(screen.getByText("1/2 已装")).toBeInTheDocument();
  });
});

function record<T extends Record<string, unknown>>(
  id: string,
  resource: Resource,
  data: T
): RecordRow<T> {
  return {
    id,
    tripId: "trip-a",
    recordKey: id,
    resource,
    position: 0,
    version: 1,
    data,
    createdByMemberId: "member-a",
    updatedByMemberId: "member-a",
    createdAt: "2026-07-31T00:00:00Z",
    updatedAt: "2026-07-31T00:00:00Z"
  };
}
