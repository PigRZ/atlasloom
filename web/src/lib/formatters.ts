// 纯格式化函数把 API 原始值转换为稳定的中文日期、金额和文本。
const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  timeZone: "UTC"
});

const dayFormatter = new Intl.DateTimeFormat("zh-CN", {
  month: "long",
  day: "numeric",
  weekday: "short",
  timeZone: "UTC"
});

function isoDate(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
  const date = new Date(`${value}T00:00:00Z`);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatDate(value: string): string {
  const date = isoDate(value);
  return date ? dateFormatter.format(date) : value;
}

export function formatDateTime(value: string): string {
  const matched = value.match(/^(\d{4}-\d{2}-\d{2})[T ](\d{2}):(\d{2})(?::\d{2})?$/);
  if (!matched) return value;
  const [, date, hour, minute] = matched;
  return `${formatDate(date!)} ${hour}:${minute}`;
}

export function formatDayDate(value: string): string {
  const date = isoDate(value);
  return date ? dayFormatter.format(date) : value;
}

export function formatDayTitle(value: string): string {
  const withoutIndex = value.replace(/^D\d+\s*/i, "").trim();
  return withoutIndex || value;
}

export function formatDateRange(start: string, end: string): string {
  return `${formatDate(start)} 至 ${formatDate(end)}`;
}

export function formatMoneyMinor(value: unknown, currency: string): string {
  const amount = Number(value);
  if (!Number.isFinite(amount)) return String(value ?? "");
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency,
    minimumFractionDigits: 2
  }).format(amount / 100);
}

export function formatDistanceMeters(value: number): string {
  const formatter = new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: value >= 100_000 ? 0 : 1
  });
  return value < 1000
    ? `${formatter.format(Math.round(value))} 米`
    : `${formatter.format(value / 1000)} 公里`;
}

export function formatDurationSeconds(value: number): string {
  const totalMinutes = Math.max(1, Math.round(value / 60));
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours === 0) return `${totalMinutes} 分钟`;
  if (minutes === 0) return `${hours} 小时`;
  return `${hours} 小时 ${minutes} 分钟`;
}
