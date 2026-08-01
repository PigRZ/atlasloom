// 操作记录按时间倒序展示操作者、模块和真正变化的字段。
import { ClockCounterClockwise } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { DataError, PageSkeleton } from "../../components/PageStates";
import { api } from "../../lib/api";
import type { AuditLog, Trip } from "../../lib/types";

export function AuditLogPage({ trip }: { trip: Trip }) {
  const logs = useQuery({
    queryKey: ["audit-logs", trip.id],
    queryFn: () => api.auditLogs(trip.id)
  });
  const members = useQuery({
    queryKey: ["library", "members"],
    queryFn: () => api.libraryRecords("members")
  });

  if (logs.isLoading || members.isLoading) return <PageSkeleton />;
  if (logs.isError || members.isError) {
    return <DataError message="操作记录读取失败" onRetry={() => Promise.all([logs.refetch(), members.refetch()])} />;
  }
  const names = new Map((members.data?.records ?? []).map((member) => [
    member.id,
    String(member.data.displayName ?? member.recordKey)
  ]));
  const rows = logs.data?.records ?? [];

  return (
    <div className="audit-page page-enter">
      <section className="page-heading">
        <div><h1>操作记录</h1></div>
      </section>
      {rows.length ? (
        <ol className="audit-list">
          {rows.map((row) => (
            <li key={row.id}>
              <i aria-hidden="true"><ClockCounterClockwise size={18} /></i>
              <article>
                <header>
                  <div>
                    <b>{names.get(row.memberId) ?? "历史成员"}</b>
                    <span>{row.action}{row.module}</span>
                    {row.recordLabel && <strong>{row.recordLabel}</strong>}
                  </div>
                  <time dateTime={row.createdAt}>{formatTimestamp(row.createdAt)}</time>
                </header>
                <AuditDiff row={row} />
              </article>
            </li>
          ))}
        </ol>
      ) : (
        <section className="empty-panel">
          <ClockCounterClockwise aria-hidden="true" size={32} weight="duotone" />
          <h2>还没有操作记录</h2>
        </section>
      )}
    </div>
  );
}

function AuditDiff({ row }: { row: AuditLog }) {
  const before = row.before ?? {};
  const after = row.after ?? {};
  const fields = [...new Set([...Object.keys(before), ...Object.keys(after)])]
    .filter((key) => JSON.stringify(before[key]) !== JSON.stringify(after[key]));
  if (!fields.length) return null;
  return (
    <dl className="audit-diff">
      {fields.map((field) => (
        <div key={field}>
          <dt>{fieldLabel(field)}</dt>
          <dd>
            {before[field] !== undefined && <del>{displayValue(before[field])}</del>}
            {after[field] !== undefined && <ins>{displayValue(after[field])}</ins>}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function displayValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "未设置";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (Array.isArray(value)) return value.length ? value.map(displayValue).join("、") : "无";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : new Intl.DateTimeFormat("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit"
    }).format(date);
}

const fieldLabels: Record<string, string> = {
  title: "标题",
  date: "日期",
  name: "名称",
  type: "类型",
  item: "项目",
  amountMinor: "金额",
  payerMemberId: "支付人",
  splitMemberIds: "分摊成员",
  placeEntries: "日程地点",
  packed: "已携带",
  status: "状态",
  mobile: "手机号",
  alternateContact: "备用联系方式",
  emergencyContact: "紧急联系人联系方式"
};

function fieldLabel(field: string): string {
  return fieldLabels[field] ?? field;
}
