import { describe, expect, it } from "vitest";
import {
  expenseShares,
  memberExpenseSummaries,
  settlementTransfers
} from "./expenseModel";
import type { RecordRow } from "./types";

function row(id: string, resource: RecordRow["resource"], data: Record<string, unknown>): RecordRow {
  return {
    id, tripId: "trip-a", recordKey: id, resource, position: 0, version: 1,
    data, createdByMemberId: "member-a", updatedByMemberId: "member-a",
    createdAt: "", updatedAt: ""
  };
}

describe("费用分摊", () => {
  it("把余分稳定分给前面的成员", () => {
    const shares = expenseShares(row("expense-a", "expenses", {
      item: "停车费",
      amountMinor: 100,
      payerMemberId: "member-a",
      splitMemberIds: ["member-a", "member-b", "member-c"]
    }));
    expect(shares.map((share) => share.amountMinor)).toEqual([34, 33, 33]);
  });

  it("根据支付和应分摊金额生成结算转账", () => {
    const members = [
      row("trip-member-a", "members", { memberId: "member-a" }),
      row("trip-member-b", "members", { memberId: "member-b" })
    ];
    const expenses = [row("expense-a", "expenses", {
      item: "晚餐",
      amountMinor: 200,
      payerMemberId: "member-a",
      splitMemberIds: ["member-a", "member-b"]
    })];
    const summaries = memberExpenseSummaries(members, expenses);
    expect(summaries.find((item) => item.memberId === "member-a")?.netMinor).toBe(100);
    expect(settlementTransfers(summaries)).toEqual([{
      fromMemberId: "member-b",
      toMemberId: "member-a",
      amountMinor: 100
    }]);
  });
});
