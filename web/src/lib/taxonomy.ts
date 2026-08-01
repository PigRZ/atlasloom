// 通用分类代码与中文展示集中维护，实例专有词汇使用自定义标签。
import type { Resource, TransportMode } from "./types";

export type TaxonomyOption = { value: string; label: string };

export const transportOptions: Array<TaxonomyOption & { value: TransportMode }> = [
  { value: "walk", label: "步行" },
  { value: "drive", label: "自驾" },
  { value: "taxi", label: "出租车" },
  { value: "rideshare", label: "网约车" },
  { value: "public_transit", label: "公共交通" },
  { value: "train", label: "火车" },
  { value: "flight", label: "飞机" },
  { value: "shuttle", label: "接驳车" },
  { value: "scenic_bus", label: "景区交通" },
  { value: "ferry", label: "轮渡" },
  { value: "bicycle", label: "骑行" },
  { value: "other", label: "其他" }
];

const statusOptions: TaxonomyOption[] = [
  { value: "planned", label: "已规划" },
  { value: "needs_booking", label: "待预订" },
  { value: "booked", label: "已预订" },
  { value: "confirmed", label: "已确认" },
  { value: "in_progress", label: "进行中" },
  { value: "completed", label: "已完成" },
  { value: "skipped", label: "已跳过" },
  { value: "cancelled", label: "已取消" }
];

const fieldOptions: Record<string, TaxonomyOption[]> = {
  status: statusOptions,
  mode: transportOptions,
  splitMode: [
    { value: "selected", label: "指定成员" },
    { value: "all", label: "全体成员" }
  ]
};

export function taxonomyOptions(resource: Resource, field: string): TaxonomyOption[] {
  if (field === "status" && resource === "bookings") {
    return [
      { value: "needs_booking", label: "待预订" },
      { value: "booked", label: "已预订" },
      { value: "cancelled", label: "已取消" }
    ];
  }
  if (field === "category" && resource === "packing-items") {
    return [
      { value: "clothing", label: "衣物" },
      { value: "document", label: "证件" },
      { value: "medicine", label: "药品" },
      { value: "electronics", label: "电子设备" },
      { value: "daily_use", label: "日用品" },
      { value: "supply", label: "补给" },
      { value: "gear", label: "装备" },
      { value: "vehicle", label: "车辆用品" },
      { value: "other", label: "其他" }
    ];
  }
  return fieldOptions[field] ?? [];
}

export function taxonomyLabel(resource: Resource, field: string, value: unknown): string {
  const text = String(value ?? "");
  return taxonomyOptions(resource, field).find((option) => option.value === text)?.label
    ?? localizedText(text);
}

export function transportLabel(mode: string): string {
  return transportOptions.find((option) => option.value === mode)?.label ?? "其他";
}

export function localizedText(value: unknown): string {
  const text = String(value ?? "");
  if (!text) return "";
  return /^[a-z][a-z0-9_-]*$/.test(text) ? "其他" : text;
}
