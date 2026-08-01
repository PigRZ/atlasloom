// 分类展示测试确保当前稳定代码不会直接泄漏到中文界面。
import { describe, expect, it } from "vitest";
import { localizedText, taxonomyLabel } from "./taxonomy";

describe("中文分类展示", () => {
  it("翻译当前固定分类", () => {
    expect(taxonomyLabel("bookings", "status", "needs_booking")).toBe("待预订");
    expect(taxonomyLabel("packing-items", "category", "daily_use")).toBe("日用品");
    expect(taxonomyLabel("segments", "mode", "drive")).toBe("自驾");
  });

  it("保留中文与班次标识，隐藏未知英文代码", () => {
    expect(localizedText("示例景区")).toBe("示例景区");
    expect(localizedText("C5796")).toBe("C5796");
    expect(localizedText("unknown_code")).toBe("其他");
  });
});
