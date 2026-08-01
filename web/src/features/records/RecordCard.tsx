// 记录卡优先展示行动字段，并把编辑交给统一浮层。
import { ArrowSquareOut, PencilSimple, Trash } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { formatDate, formatDateTime, formatMoneyMinor } from "../../lib/formatters";
import {
  resourceCoreFields,
  type FieldType,
  type ResourceDefinition
} from "../../lib/resourceDefinitions";
import { taxonomyLabel } from "../../lib/taxonomy";
import type { RecordRow } from "../../lib/types";

type RecordCardProps = {
  row: RecordRow;
  currency: string;
  definition: ResourceDefinition;
  displayLabels: Map<string, string>;
  highlighted?: boolean;
  audit?: { createdBy: string; updatedBy: string };
  onEdit: () => void;
  onDelete: () => void;
  linkedExpense?: RecordRow;
  linkedExpenseHref?: string;
  linkedBooking?: RecordRow;
  hideDelete?: boolean;
  readOnly?: boolean;
};

const maintenanceFields = new Set(["latitude", "longitude", "weatherAdcode"]);

export function RecordCard({
  row,
  currency,
  definition,
  displayLabels,
  highlighted,
  audit,
  onEdit,
  onDelete,
  linkedExpense,
  linkedExpenseHref,
  linkedBooking,
  hideDelete,
  readOnly
}: RecordCardProps) {
  const primaryField = definition.fields[0]?.[0] ?? "recordKey";
  const coreFields = new Set(resourceCoreFields[definition.id]);
  const visibleFields = definition.fields
    .slice(1)
    .filter(([key]) => coreFields.has(key))
    .filter(([key]) => !maintenanceFields.has(key))
    .filter(([, , type]) => !["url", "url-list", "tel"].includes(type))
    .filter(([key]) => row.data[key] !== undefined && row.data[key] !== "")
    .slice(0, 4);
  const actionFields = definition.fields.filter(([key, , type]) => (
    (type === "url" || type === "url-list" || type === "tel") && Boolean(row.data[key])
  ));

  return (
    <article className={`record-card${highlighted ? " is-highlighted" : ""}`}>
      <div className="record-head">
        <div className="record-title">
          <h3>
            {displayLabels.get(String(row.data[primaryField] ?? ""))
              ?? String(row.data[primaryField] ?? "未命名")}
          </h3>
        </div>
        {!readOnly && (
          <div className="record-actions">
            <button type="button" onClick={onEdit}>
              <PencilSimple aria-hidden="true" size={15} />
              编辑
            </button>
            {!hideDelete && (
              <button type="button" className="danger" onClick={onDelete}>
                <Trash aria-hidden="true" size={15} />
                删除
              </button>
            )}
          </div>
        )}
      </div>
      {visibleFields.length > 0 && (
        <dl>
          {visibleFields.map(([key, label, type]) => (
            <div key={key} className={["category", "status", "type"].includes(key)
              ? "record-category-field"
              : undefined}
            >
              <dt>{label}</dt>
              <dd>{fieldValue(row, key, label, type, currency, displayLabels)}</dd>
            </div>
          ))}
        </dl>
      )}
      {actionFields.length > 0 && (
        <div className="record-action-links">
          {actionFields.map(([key, label, type]) => {
            const values = type === "url-list" && Array.isArray(row.data[key])
              ? row.data[key].map(String)
              : [String(row.data[key] ?? "")];
            return values.map((value, index) => {
              const href = type === "tel" ? `tel:${value}` : value;
              return (
                <a
                  href={href}
                  target={type !== "tel" ? "_blank" : undefined}
                  rel={type !== "tel" ? "noreferrer" : undefined}
                  key={`${key}:${index}`}
                >
                  {type === "tel" ? `拨打${label}` : `打开${label}${values.length > 1 ? ` ${index + 1}` : ""}`}
                  {type !== "tel" && <ArrowSquareOut aria-hidden="true" size={13} />}
                </a>
              );
            });
          })}
        </div>
      )}
      {definition.id === "bookings" && (
        <BookingExpenseLink
          expense={linkedExpense}
          href={linkedExpenseHref}
          currency={currency}
          displayLabels={displayLabels}
        />
      )}
      {definition.id === "expenses" && linkedBooking && (
        <LinkedBookingDetails booking={linkedBooking} displayLabels={displayLabels} />
      )}
      {audit && (
        <p className="record-audit">
          创建：{audit.createdBy}<span aria-hidden="true">·</span>最近修改：{audit.updatedBy}
        </p>
      )}
    </article>
  );
}

function LinkedBookingDetails({
  booking,
  displayLabels
}: {
  booking: RecordRow;
  displayLabels: Map<string, string>;
}) {
  const item = String(booking.data.item ?? "未命名预订");
  const place = displayLabels.get(String(booking.data.placeId ?? ""));
  const status = taxonomyLabel("bookings", "status", booking.data.status);
  const dateTime = String(booking.data.dateTime ?? "");
  const notes = String(booking.data.notes ?? "");
  const link = String(booking.data.link ?? "");

  return (
    <details className="linked-record-details">
      <summary>
        <span>来源预订</span>
        <b>{item}</b>
      </summary>
      <dl>
        {place && <div><dt>地点</dt><dd>{place}</dd></div>}
        {dateTime && <div><dt>日期 / 时间</dt><dd>{formatDateTime(dateTime)}</dd></div>}
        {status && <div><dt>状态</dt><dd>{status}</dd></div>}
        {notes && <div><dt>备注</dt><dd>{notes}</dd></div>}
      </dl>
      {link && (
        <a className="inline-link" href={link} target="_blank" rel="noreferrer">
          打开订单链接
          <ArrowSquareOut aria-hidden="true" size={13} />
        </a>
      )}
    </details>
  );
}

function BookingExpenseLink({
  expense,
  href,
  currency,
  displayLabels
}: {
  expense?: RecordRow;
  href?: string;
  currency: string;
  displayLabels: Map<string, string>;
}) {
  if (!expense) {
    return <p className="booking-cost-link">费用项生成中</p>;
  }
  const missing = [
    !expense.data.amountMinor ? "金额" : "",
    !expense.data.payerMemberId ? "支付人" : ""
  ].filter(Boolean);
  const payer = displayLabels.get(String(expense.data.payerMemberId ?? "")) ?? "未设置";
  const summary = missing.length
    ? `费用待登记：缺少${missing.join("、")}`
    : `费用已登记：${formatMoneyMinor(expense.data.amountMinor, currency)} · ${payer}支付`;

  return (
    <div className={`booking-cost-link${missing.length ? "" : " linked"}`}>
      <span>{summary}</span>
      {href && <Link to={href}>{missing.length ? "登记费用" : "查看费用"}</Link>}
    </div>
  );
}

function fieldValue(
  row: RecordRow,
  key: string,
  label: string,
  type: FieldType,
  currency: string,
  displayLabels: Map<string, string>
): ReactNode {
  const value = row.data[key];
  if (type === "date") return formatDate(String(value ?? ""));
  if (type === "datetime-local") return formatDateTime(String(value ?? ""));
  if (type === "checkbox") return value ? "是" : "否";
  if (key === "amountMinor") return formatMoneyMinor(value, currency);
  if (type === "tags") {
    const tags = Array.isArray(value) ? value.map(String) : [];
    return tags.length ? tags.join("、") : "未设置";
  }
  if (type === "multiselect") {
    const ids = Array.isArray(value) ? value.map(String) : [];
    return ids.length ? ids.map((id) => displayLabels.get(id) ?? id).join("、") : "未指定";
  }
  if ([
    "category",
    "type",
    "bookerMemberId",
    "payerMemberId",
    "ownerMemberId",
    "recordedByMemberId",
    "bookingRecordId",
    "dayId",
    "placeId",
    "platformId"
  ].includes(key)) {
    return displayLabels.get(String(value ?? ""))
      ?? taxonomyLabel(row.resource, key, value)
      ?? "未设置";
  }
  if (type === "select") return taxonomyLabel(row.resource, key, value);
  if (type === "url" && typeof value === "string") {
    return (
      <a
        className="inline-link"
        href={value}
        target="_blank"
        rel="noreferrer"
        aria-label={`${label}：打开`}
      >
        打开{label}
        <ArrowSquareOut aria-hidden="true" size={13} />
      </a>
    );
  }
  return String(value ?? "");
}
