// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { ConfirmProvider, useConfirm } from "./ConfirmDialog";

function NoticeHarness() {
  const confirm = useConfirm();
  return (
    <button
      type="button"
      onClick={() => confirm({
        title: "暂时不能删除",
        message: "请先处理引用。",
        confirmLabel: "知道了",
        acknowledgeOnly: true
      })}
    >
      打开提示
    </button>
  );
}

function ConfirmHarness() {
  const confirm = useConfirm();
  return (
    <button
      type="button"
      onClick={() => confirm({
        title: "放弃修改？",
        message: "未保存内容不会保留。",
        confirmLabel: "放弃修改",
        tone: "danger"
      })}
    >
      打开确认
    </button>
  );
}

describe("ConfirmDialog", () => {
  it("supports a focused one-action notice for blocked destructive actions", async () => {
    render(
      <ConfirmProvider>
        <NoticeHarness />
      </ConfirmProvider>
    );

    fireEvent.click(screen.getByRole("button", { name: "打开提示" }));
    expect(screen.getByRole("alertdialog")).toHaveTextContent("请先处理引用。");
    expect(screen.queryByRole("button", { name: "取消" })).not.toBeInTheDocument();
    const acknowledge = screen.getByRole("button", { name: "知道了" });
    await waitFor(() => expect(acknowledge).toHaveFocus());

    fireEvent.click(acknowledge);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("traps keyboard focus and restores the opener after closing", async () => {
    const user = userEvent.setup();
    render(
      <ConfirmProvider>
        <ConfirmHarness />
      </ConfirmProvider>
    );

    const opener = screen.getByRole("button", { name: "打开确认" });
    await user.click(opener);
    const cancel = screen.getByRole("button", { name: "取消" });
    const confirm = screen.getByRole("button", { name: "放弃修改" });
    await waitFor(() => expect(cancel).toHaveFocus());

    await user.tab({ shift: true });
    expect(confirm).toHaveFocus();
    await user.tab();
    expect(cancel).toHaveFocus();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    await waitFor(() => expect(opener).toHaveFocus());
  });
});
