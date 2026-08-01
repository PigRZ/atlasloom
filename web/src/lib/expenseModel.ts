// 费用结算只由“谁支付、谁分摊”推导，不保存额外结算状态。
import type { RecordRow } from "./types";

export type ExpenseShare = {
  expenseId: string;
  expenseItem: string;
  memberId: string;
  payerMemberId: string;
  amountMinor: number;
};

export type MemberExpenseSummary = {
  memberId: string;
  paidMinor: number;
  owedMinor: number;
  netMinor: number;
  expenseIds: string[];
};

export type SettlementTransfer = {
  fromMemberId: string;
  toMemberId: string;
  amountMinor: number;
};

export function listValue(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean);
  return String(value ?? "")
    .split(/[，,、]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function expenseShares(expense: RecordRow): ExpenseShare[] {
  const participants = listValue(expense.data.splitMemberIds)
    .filter((memberId, index, items) => items.indexOf(memberId) === index);
  const payerMemberId = String(expense.data.payerMemberId ?? "");
  const amountMinor = Number(expense.data.amountMinor ?? 0);
  if (!Number.isInteger(amountMinor) || amountMinor <= 0 || !payerMemberId || !participants.length) return [];
  const base = Math.floor(amountMinor / participants.length);
  const remainder = amountMinor % participants.length;
  return participants.map((memberId, index) => ({
    expenseId: expense.id,
    expenseItem: String(expense.data.item ?? "未命名费用"),
    memberId,
    payerMemberId,
    amountMinor: base + (index < remainder ? 1 : 0)
  }));
}

export function memberExpenseSummaries(
  members: RecordRow[],
  expenses: RecordRow[]
): MemberExpenseSummary[] {
  const summaries = new Map(members.map((member) => [
    String(member.data.memberId ?? member.id),
    {
      memberId: String(member.data.memberId ?? member.id),
      paidMinor: 0,
      owedMinor: 0,
      netMinor: 0,
      expenseIds: [] as string[]
    }
  ]));
  expenses.forEach((expense) => {
    const payer = String(expense.data.payerMemberId ?? "");
    const amount = Number(expense.data.amountMinor ?? 0);
    if (payer && Number.isInteger(amount) && amount > 0) {
      const summary = summaries.get(payer);
      if (summary) {
        summary.paidMinor += amount;
        summary.expenseIds.push(expense.id);
      }
    }
    expenseShares(expense).forEach((share) => {
      const summary = summaries.get(share.memberId);
      if (summary) {
        summary.owedMinor += share.amountMinor;
        summary.expenseIds.push(expense.id);
      }
    });
  });
  return [...summaries.values()].map((summary) => ({
    ...summary,
    netMinor: summary.paidMinor - summary.owedMinor,
    expenseIds: [...new Set(summary.expenseIds)]
  }));
}

export function settlementTransfers(summaries: MemberExpenseSummary[]): SettlementTransfer[] {
  const debtors = summaries
    .filter((summary) => summary.netMinor < 0)
    .map((summary) => ({ memberId: summary.memberId, amount: -summary.netMinor }));
  const creditors = summaries
    .filter((summary) => summary.netMinor > 0)
    .map((summary) => ({ memberId: summary.memberId, amount: summary.netMinor }));
  const transfers: SettlementTransfer[] = [];
  let debtorIndex = 0;
  let creditorIndex = 0;
  while (debtors[debtorIndex] && creditors[creditorIndex]) {
    const debtor = debtors[debtorIndex]!;
    const creditor = creditors[creditorIndex]!;
    const amountMinor = Math.min(debtor.amount, creditor.amount);
    transfers.push({
      fromMemberId: debtor.memberId,
      toMemberId: creditor.memberId,
      amountMinor
    });
    debtor.amount -= amountMinor;
    creditor.amount -= amountMinor;
    if (debtor.amount === 0) debtorIndex += 1;
    if (creditor.amount === 0) creditorIndex += 1;
  }
  return transfers;
}
