// 格式化测试固定中文日期、金额和空值的用户可见表达。
import { describe, expect, it } from "vitest";
import {
  formatDate,
  formatDateTime,
  formatDateRange,
  formatDayDate,
  formatDayTitle,
  formatDistanceMeters,
  formatDurationSeconds,
  formatMoneyMinor
} from "./formatters";

describe("formatters", () => {
  it("formats persisted ISO dates without relying on string slicing", () => {
    expect(formatDate("2026-09-30")).toBe("2026/09/30");
    expect(formatDayDate("2026-09-30")).toContain("9月30日");
    expect(formatDateRange("2026-09-30", "2026-10-09")).toBe("2026/09/30 至 2026/10/09");
  });

  it("keeps invalid dates visible and formats minor currency units", () => {
    expect(formatDate("待确认")).toBe("待确认");
    expect(formatDayTitle("D3 车站→酒店")).toBe("车站→酒店");
    expect(formatDayTitle("示例景区全天")).toBe("示例景区全天");
    expect(formatMoneyMinor(1234, "CNY")).toBe("¥12.34");
  });

  it("formats local date-times and keeps non-standard source text", () => {
    expect(formatDateTime("2026-09-25T18:00")).toBe("2026/09/25 18:00");
    expect(formatDateTime("2026-09-25 08:30:00")).toBe("2026/09/25 08:30");
    expect(formatDateTime("2026-09-28至09-30")).toBe("2026-09-28至09-30");
  });

  it("formats route distance and duration for quick scanning", () => {
    expect(formatDistanceMeters(850)).toBe("850 米");
    expect(formatDistanceMeters(12_340)).toBe("12.3 公里");
    expect(formatDurationSeconds(7_500)).toBe("2 小时 5 分钟");
  });
});
