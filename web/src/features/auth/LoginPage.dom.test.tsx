// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../../lib/api";
import type { Session } from "../../lib/types";
import { LoginPage } from "./LoginPage";

const session: Session = {
  user: {
    id: "member-a",
    displayName: "同行成员",
    mobile: "13800138000",
    admin: false
  },
  csrfToken: "csrf-token",
  expiresAt: "2026-08-01T00:00:00Z"
};

function renderLogin(onSession = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } }
  });
  render(
    <QueryClientProvider client={queryClient}>
      <LoginPage onSession={onSession} />
    </QueryClientProvider>
  );
  return onSession;
}

afterEach(() => vi.restoreAllMocks());

describe("LoginPage", () => {
  it("过滤手机号输入并建立会话", async () => {
    const login = vi.spyOn(api, "login").mockResolvedValue(session);
    const onSession = renderLogin();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("手机号"), "a138-0013-8000");
    await user.type(screen.getByLabelText("密码"), "safe-password");
    expect(screen.getByLabelText("手机号")).toHaveValue("13800138000");

    await user.click(screen.getByRole("button", { name: "显示密码" }));
    expect(screen.getByLabelText("密码")).toHaveAttribute("type", "text");
    await user.click(screen.getByRole("button", { name: "登录" }));

    await waitFor(() => expect(login).toHaveBeenCalledWith("13800138000", "safe-password"));
    expect(onSession).toHaveBeenCalledWith(session);
  });

  it("把接口错误作为可访问提示显示", async () => {
    vi.spyOn(api, "login").mockRejectedValue(new Error("手机号或密码不正确"));
    renderLogin();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("手机号"), "13800138000");
    await user.type(screen.getByLabelText("密码"), "wrong-password");
    await user.click(screen.getByRole("button", { name: "登录" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("手机号或密码不正确");
  });
});
