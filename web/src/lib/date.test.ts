import { describe, expect, it } from "vitest";
import { currentDateInTimezone, inclusiveCalendarDays } from "./date";

describe("calendar date helpers", () => {
  it("uses the trip timezone instead of the UTC calendar day", () => {
    const now = new Date("2026-07-27T16:30:00Z");
    expect(currentDateInTimezone("Asia/Shanghai", now)).toBe("2026-07-28");
    expect(currentDateInTimezone("UTC", now)).toBe("2026-07-27");
  });

  it("counts an inclusive date range across a month boundary", () => {
    expect(inclusiveCalendarDays("2026-09-29", "2026-10-01")).toBe(3);
    expect(inclusiveCalendarDays("2026-10-02", "2026-10-01")).toBe(0);
  });
});
