// 导航定义集中维护页面标识、深链解析与展示元数据。
import {
  Backpack,
  CalendarDots,
  ClockCounterClockwise,
  Database,
  DotsThree,
  House,
  MapPin,
  MapTrifold,
  Notebook,
  Ticket,
  UsersThree,
  Wallet,
  type Icon
} from "@phosphor-icons/react";
import type { PageResource } from "../lib/types";

export type Page = "trips" | "library" | "today" | "itinerary" | "map" | "more" | "audit" | PageResource;
export type NavPage = { id: Page; label: string; icon: Icon };
export type AppRoute = {
  page: Page;
  tripId: string;
  date: string;
};

export const navGroups: Array<{ label: string; pages: NavPage[] }> = [
  {
    label: "执行",
    pages: [
      { id: "today", label: "今天", icon: House },
      { id: "itinerary", label: "日程", icon: CalendarDots }
    ]
  },
  {
    label: "规划",
    pages: [
      { id: "places", label: "地点", icon: MapPin },
      { id: "bookings", label: "预订", icon: Ticket },
      { id: "packing-items", label: "物品", icon: Backpack }
    ]
  },
  {
    label: "协作",
    pages: [
      { id: "expenses", label: "费用", icon: Wallet },
      { id: "members", label: "成员", icon: UsersThree },
      { id: "research-evidence", label: "资料", icon: Notebook },
      { id: "audit", label: "操作记录", icon: ClockCounterClockwise }
    ]
  }
];

export const desktopPrimaryPages = navGroups.flatMap((group) => group.pages);

export const mobilePrimaryPages: NavPage[] = [
  { id: "today", label: "今天", icon: House },
  { id: "itinerary", label: "日程", icon: CalendarDots },
  { id: "expenses", label: "费用", icon: Wallet },
  { id: "more", label: "更多", icon: DotsThree }
];

export const mobileMorePages = desktopPrimaryPages.filter((page) => (
  page.id !== "today"
  && page.id !== "itinerary"
  && page.id !== "expenses"
));

export const allPages = [
  ...desktopPrimaryPages,
  { id: "map" as const, label: "地图", icon: MapTrifold },
  { id: "more" as const, label: "更多", icon: DotsThree }
];

const knownPages = new Set(allPages.map((page) => page.id));

export function appRouteFromPath(pathname: string): AppRoute {
  const parts = pathname.split("/").filter(Boolean);
  const appIndex = parts.lastIndexOf("app");
  const route = appIndex >= 0 ? parts.slice(appIndex + 1) : [];
  if (route[0] === "trips" && !route[1]) {
    return { page: "trips", tripId: "", date: "" };
  }
  if (route[0] === "library") {
    return { page: "library", tripId: "", date: "" };
  }
  if (route[0] !== "trips" || !route[1]) {
    return { page: "today", tripId: "", date: "" };
  }
  if (route[2] === "day") {
    return {
      page: "today",
      tripId: decodeURIComponent(route[1]),
      date: /^\d{4}-\d{2}-\d{2}$/.test(route[3] ?? "") ? route[3]! : ""
    };
  }
  const page = knownPages.has(route[2] as Page) ? route[2] as Page : "today";
  return {
    page,
    tripId: decodeURIComponent(route[1]),
    date: ""
  };
}

export function pagePath(tripId: string, page: Page, date?: string): string {
  if (page === "trips") return "/app/trips";
  if (page === "library") return "/app/library";
  const trip = encodeURIComponent(tripId);
  if (page === "today") {
    return date
      ? `/app/trips/${trip}/day/${date}`
      : `/app/trips/${trip}/day`;
  }
  return `/app/trips/${trip}/${page}`;
}

export function pagePathForContext(tripId: string, page: Page, date: string): string {
  if (page === "trips" || page === "library") return pagePath("", page);
  if (page === "today") return pagePath(tripId, page, date || undefined);
  const path = pagePath(tripId, page);
  return date ? `${path}?date=${encodeURIComponent(date)}` : path;
}

export function pageLabel(page: Page): string {
  if (page === "trips") return "所有旅行";
  if (page === "library") return "基础资料";
  return allPages.find((item) => item.id === page)?.label ?? "今天";
}

export const libraryPage = { id: "library" as const, label: "基础资料", icon: Database };
