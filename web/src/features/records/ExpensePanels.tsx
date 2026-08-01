// 结算面板从费用事实实时计算成员净额与最少转账方案。
import { ArrowRight, UsersThree } from "@phosphor-icons/react";
import {
  memberExpenseSummaries,
  settlementTransfers
} from "../../lib/expenseModel";
import { formatMoneyMinor } from "../../lib/formatters";
import type { RecordRow } from "../../lib/types";

export function MemberSettlement({
  members,
  expenses,
  currency,
  onOpenExpense
}: {
  members: RecordRow[];
  expenses: RecordRow[];
  currency: string;
  onOpenExpense?: (expense: RecordRow) => void;
}) {
  const names = new Map(members.map((member) => [
    String(member.data.memberId ?? member.id),
    String(member.data.displayName ?? "未命名成员")
  ]));
  const summaries = memberExpenseSummaries(members, expenses);
  const transfers = settlementTransfers(summaries);
  const expensesByID = new Map(expenses.map((expense) => [expense.id, expense]));
  const validExpenseCount = expenses.filter((expense) => Number(expense.data.amountMinor ?? 0) > 0).length;
  if (!expenses.length) {
    return (
      <section className="settlement-panel empty-settlement">
        <UsersThree aria-hidden="true" size={28} weight="duotone" />
        <div><b>还没有费用</b><p>登记付款人与分摊成员后，这里会自动计算。</p></div>
      </section>
    );
  }
  return (
    <section className="settlement-panel" aria-labelledby="settlement-title">
      <div className="panel-heading">
        <div>
          <h2 id="settlement-title">费用分摊</h2>
          <span>{validExpenseCount} 笔已登记费用</span>
        </div>
      </div>
      <div className="member-balance-grid">
        {summaries.map((summary) => (
          <article key={summary.memberId}>
            <header>
              <b>{names.get(summary.memberId) ?? "未知成员"}</b>
              <strong className={summary.netMinor >= 0 ? "is-credit" : "is-debt"}>
                {summary.netMinor >= 0 ? "应收 " : "应付 "}
                {formatMoneyMinor(Math.abs(summary.netMinor), currency)}
              </strong>
            </header>
            <p>
              已支付 {formatMoneyMinor(summary.paidMinor, currency)}
              <span>·</span>
              应分摊 {formatMoneyMinor(summary.owedMinor, currency)}
            </p>
            <div>
              {summary.expenseIds.map((expenseID) => {
                const expense = expensesByID.get(expenseID);
                if (!expense) return null;
                const label = String(expense.data.item ?? "未命名费用");
                return onOpenExpense
                  ? (
                    <button type="button" key={expenseID} onClick={() => onOpenExpense(expense)}>
                      {label}<ArrowRight aria-hidden="true" size={13} />
                    </button>
                  )
                  : <span className="expense-chip" key={expenseID}>{label}</span>;
              })}
            </div>
          </article>
        ))}
      </div>
      <div className="settlement-transfers">
        <h3>建议结算</h3>
        {transfers.length ? transfers.map((transfer, index) => (
          <p key={`${transfer.fromMemberId}:${transfer.toMemberId}:${index}`}>
            <b>{names.get(transfer.fromMemberId) ?? "未知成员"}</b>
            <span>转给</span>
            <b>{names.get(transfer.toMemberId) ?? "未知成员"}</b>
            <strong>{formatMoneyMinor(transfer.amountMinor, currency)}</strong>
          </p>
        )) : <p>当前没有需要结算的差额。</p>}
      </div>
    </section>
  );
}
