// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { ConfirmProvider } from "./ConfirmDialog";
import { EditorSurface } from "./EditorSurface";

type ControlKind = "text" | "textarea" | "select" | "datetime-local" | "checkbox";

function Harness({ kind }: { kind: ControlKind }) {
  const [dirty, setDirty] = useState(false);
  const [value, setValue] = useState("");
  const [checked, setChecked] = useState(false);
  const update = (nextValue: string) => {
    setValue(nextValue);
    setDirty(true);
  };

  return (
    <ConfirmProvider>
      <EditorSurface
        open
        title="测试编辑器"
        variant="modal"
        dirty={dirty}
        onClose={vi.fn()}
      >
        {kind === "textarea" && (
          <label>
            备注
            <textarea
              value={value}
              onChange={(event) => update(event.target.value)}
            />
          </label>
        )}
        {kind === "select" && (
          <label>
            状态
            <select
              value={value}
              onChange={(event) => update(event.target.value)}
            >
              <option value="">未设置</option>
              <option value="booked">已预订</option>
            </select>
          </label>
        )}
        {kind === "checkbox" && (
          <label>
            <input
              type="checkbox"
              checked={checked}
              onChange={(event) => {
                setChecked(event.target.checked);
                setDirty(true);
              }}
            />
            已完成
          </label>
        )}
        {(kind === "text" || kind === "datetime-local") && (
          <label>
            {kind === "text" ? "名称" : "日期 / 时间"}
            <input
              type={kind}
              value={value}
              onChange={(event) => update(event.target.value)}
            />
          </label>
        )}
      </EditorSurface>
    </ConfirmProvider>
  );
}

function DismissHarness({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmProvider>
      <EditorSurface
        open={open}
        title="测试编辑器"
        variant="modal"
        dirty
        onClose={() => {
          onClose();
          setOpen(false);
        }}
      >
        <input aria-label="测试字段" />
      </EditorSurface>
    </ConfirmProvider>
  );
}

function NestedHarness({
  onParentClose,
  onNestedClose
}: {
  onParentClose: () => void;
  onNestedClose: () => void;
}) {
  const [nestedOpen, setNestedOpen] = useState(false);
  return (
    <ConfirmProvider>
      <EditorSurface
        open
        title="父级编辑器"
        variant="drawer"
        dirty
        onClose={onParentClose}
      >
        <button type="button" onClick={() => setNestedOpen(true)}>新增地点</button>
      </EditorSurface>
      <EditorSurface
        open={nestedOpen}
        title="新增地点"
        variant="modal"
        layer="nested"
        dirty={false}
        onClose={() => {
          onNestedClose();
          setNestedOpen(false);
        }}
      >
        <input aria-label="地点名称" />
      </EditorSurface>
    </ConfirmProvider>
  );
}

const cases: Array<{
  kind: ControlKind;
  label: string;
  change: (control: HTMLElement) => void;
}> = [
  {
    kind: "text",
    label: "名称",
    change: (control) => fireEvent.change(control, { target: { value: "首" } })
  },
  {
    kind: "textarea",
    label: "备注",
    change: (control) => fireEvent.change(control, { target: { value: "首" } })
  },
  {
    kind: "select",
    label: "状态",
    change: (control) => fireEvent.change(control, { target: { value: "booked" } })
  },
  {
    kind: "datetime-local",
    label: "日期 / 时间",
    change: (control) => fireEvent.change(control, {
      target: { value: "2026-09-28T08:30" }
    })
  },
  {
    kind: "checkbox",
    label: "已完成",
    change: (control) => fireEvent.click(control)
  }
];

describe("EditorSurface", () => {
  it.each(cases)(
    "$kind 首次修改后保留当前控件焦点",
    ({ kind, label, change }) => {
      render(<Harness kind={kind} />);
      const control = screen.getByLabelText(label);

      control.focus();
      expect(control).toHaveFocus();
      change(control);

      expect(control).toHaveFocus();
      expect(screen.getByRole("button", { name: "关闭测试编辑器" })).not.toHaveFocus();
    }
  );

  it("首次修改后按 Escape 仍按最新脏状态确认", () => {
    render(<Harness kind="text" />);
    const control = screen.getByLabelText("名称");

    control.focus();
    fireEvent.change(control, { target: { value: "首" } });
    fireEvent.keyDown(document, { key: "Escape" });

    expect(screen.getByRole("alertdialog")).toHaveTextContent("放弃未保存修改？");
  });

  it("点击空白区确认一次后关闭编辑器", async () => {
    const onClose = vi.fn();
    render(<DismissHarness onClose={onClose} />);
    const backdrop = screen.getByRole("dialog").parentElement;
    if (!backdrop) throw new Error("缺少编辑器遮罩");

    fireEvent.mouseDown(backdrop);
    expect(screen.getByRole("alertdialog")).toHaveTextContent(
      "当前表单有未保存的修改，关闭后不会保留。"
    );
    fireEvent.click(screen.getByRole("button", { name: "放弃修改" }));

    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("嵌套地点弹窗只响应最上层 Escape，并在关闭后恢复父表单", async () => {
    const onParentClose = vi.fn();
    const onNestedClose = vi.fn();
    render(
      <NestedHarness
        onParentClose={onParentClose}
        onNestedClose={onNestedClose}
      />
    );

    const trigger = screen.getByRole("button", { name: "新增地点" });
    await waitFor(() => expect(
      screen.getByRole("button", { name: "关闭父级编辑器" })
    ).toHaveFocus());
    trigger.focus();
    fireEvent.click(trigger);
    expect(screen.getByRole("dialog", { name: "新增地点" })).toBeVisible();
    expect(screen.getByText("父级编辑器").closest(".editor-backdrop"))
      .toHaveAttribute("aria-hidden", "true");

    fireEvent.keyDown(document, { key: "Escape" });

    await waitFor(() => expect(onNestedClose).toHaveBeenCalledOnce());
    expect(onParentClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "父级编辑器" })).toBeVisible();
    expect(trigger).toHaveFocus();
  });
});
