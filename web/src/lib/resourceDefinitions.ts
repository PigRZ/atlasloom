// 资源定义集中维护通用字段；旅行实例词汇只存在于数据，不进入产品分类。
import type { Resource } from "./types";

export type FieldType =
  | "text"
  | "date"
  | "datetime-local"
  | "time"
  | "number"
  | "checkbox"
  | "select"
  | "multiselect"
  | "money"
  | "textarea"
  | "url"
  | "url-list"
  | "tel"
  | "tags";

export type ResourceDefinition = {
  id: Resource;
  label: string;
  singular: string;
  fields: Array<[string, string, FieldType]>;
};

// 核心字段回答“这是什么、何时发生、下一步怎么做”；其余字段归入默认展开的补充信息。
export const resourceCoreFields: Record<Resource, string[]> = {
  days: ["title", "date"],
  places: ["name", "type", "address", "longitude", "latitude", "openingHours"],
  segments: ["mode", "dayId", "fromPlaceId", "toPlaceId", "durationMinutes", "additionalInfo"],
  bookings: ["item", "type", "bookerMemberId", "placeId", "dateTime", "status"],
  expenses: ["item", "amountMinor", "type", "payerMemberId", "splitMode", "occurredAt"],
  "packing-items": ["item", "category", "dayIds", "quantity", "packed"],
  "research-evidence": [
    "title", "content", "platformId", "recordedByMemberId", "dayIds", "placeId"
  ],
  members: ["memberId", "role", "note"]
};

export const resources: ResourceDefinition[] = [
  {
    id: "days",
    label: "日程",
    singular: "执行日",
    fields: [
      ["title", "标题", "text"],
      ["date", "日期", "date"],
      ["additionalInfo", "补充信息", "textarea"]
    ]
  },
  {
    id: "places",
    label: "地点",
    singular: "地点",
    fields: [
      ["name", "地点名称", "text"],
      ["type", "地点类型", "select"],
      ["address", "地址", "text"],
      ["region", "地区", "text"],
      ["latitude", "纬度", "number"],
      ["longitude", "经度", "number"],
      ["weatherAdcode", "天气行政区代码", "text"],
      ["navigationURL", "导航链接", "url"],
      ["openingHours", "开放时间", "text"],
      ["recommendedTicketPlatform", "建议购票平台", "text"]
    ]
  },
  {
    id: "segments",
    label: "交通段",
    singular: "交通段",
    fields: [
      ["mode", "交通方式", "select"],
      ["dayId", "执行日", "select"],
      ["fromPlaceId", "起点地点", "select"],
      ["toPlaceId", "终点地点", "select"],
      ["durationMinutes", "预计时长（分钟，可选）", "number"],
      ["distanceMeters", "预计距离（米，可选）", "number"],
      ["departureTime", "大约出发时间（可选）", "time"],
      ["navigationURL", "外部导航 / 订单链接", "url"],
      ["additionalInfo", "补充信息", "textarea"]
    ]
  },
  {
    id: "bookings",
    label: "预订",
    singular: "预订",
    fields: [
      ["item", "项目", "text"],
      ["type", "预订类型", "select"],
      ["bookerMemberId", "预订人", "select"],
      ["placeId", "关联地点", "select"],
      ["platformId", "平台类型", "select"],
      ["dateTime", "日期 / 时间", "datetime-local"],
      ["status", "状态", "select"],
      ["link", "订单链接", "url"],
      ["notes", "备注", "textarea"]
    ]
  },
  {
    id: "expenses",
    label: "费用",
    singular: "费用",
    fields: [
      ["item", "费用项目", "text"],
      ["occurredAt", "发生时间", "datetime-local"],
      ["type", "费用类型", "select"],
      ["amountMinor", "金额", "money"],
      ["payerMemberId", "支付人", "select"],
      ["splitMode", "分摊方式", "select"],
      ["splitMemberIds", "分摊成员", "multiselect"],
      ["notes", "备注", "textarea"]
    ]
  },
  {
    id: "packing-items",
    label: "物品",
    singular: "物品",
    fields: [
      ["item", "物品", "text"],
      ["category", "物品类型", "select"],
      ["dayIds", "关联执行日", "multiselect"],
      ["quantity", "数量", "number"],
      ["packed", "已携带", "checkbox"],
      ["notes", "备注", "textarea"]
    ]
  },
  {
    id: "research-evidence",
    label: "资料",
    singular: "资料",
    fields: [
      ["title", "标题", "text"],
      ["content", "内容", "textarea"],
      ["platformId", "平台类型", "select"],
      ["recordedByMemberId", "记录人", "select"],
      ["dayIds", "适用执行日", "multiselect"],
      ["placeId", "关联地点", "select"]
    ]
  },
  {
    id: "members",
    label: "同行成员",
    singular: "同行成员",
    fields: [
      ["memberId", "选择基础成员", "select"],
      ["role", "旅行角色", "text"],
      ["note", "备注", "textarea"]
    ]
  }
];
