// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TemporalInput } from "./TemporalInput";

const originalShowPicker = HTMLInputElement.prototype.showPicker;

afterEach(() => {
  HTMLInputElement.prototype.showPicker = originalShowPicker;
});

describe("TemporalInput", () => {
  it("通过风格化按钮打开原生时间选择器并保留输入焦点", () => {
    const showPicker = vi.fn();
    HTMLInputElement.prototype.showPicker = showPicker;
    render(
      <TemporalInput
        aria-label="到达时间"
        type="time"
        value="09:30"
        onChange={vi.fn()}
      />
    );

    const input = screen.getByLabelText("到达时间");
    fireEvent.click(screen.getByRole("button", { name: "选择时间" }));

    expect(input).toHaveFocus();
    expect(showPicker).toHaveBeenCalledOnce();
  });
});
