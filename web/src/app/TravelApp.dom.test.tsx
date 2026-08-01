// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../lib/api";
import type { Session } from "../lib/types";
import { TravelApp } from "./TravelApp";

let memberASetError: ((message: string) => void) | undefined;
let memberAUpdateSession: ((session: Session) => void) | undefined;

vi.mock("../features/auth/LoginPage", () => ({
  LoginPage: ({ onSession }: { onSession: (session: Session) => void }) => (
    <button type="button" onClick={() => onSession(memberBSession)}>
      以成员乙登录
    </button>
  )
}));

vi.mock("../features/trips/TravelShell", () => ({
  TravelShell: ({
    session,
    error,
    setError,
    onLogout,
    onSessionChange
  }: {
    session: Session;
    error: string;
    setError: (message: string) => void;
    onLogout: () => void;
    onSessionChange: (session: Session) => void;
  }) => {
    if (session.user.id === memberASession.user.id) {
      memberASetError = setError;
      memberAUpdateSession = onSessionChange;
    }
    return (
      <>
        <span>当前成员：{session.user.displayName}</span>
        <span>当前错误：{error || "无"}</span>
        <button
          type="button"
          onClick={() => setError(`${session.user.displayName}的私密错误`)}
        >
          制造错误
        </button>
        <button type="button" onClick={onLogout}>退出登录</button>
      </>
    );
  }
}));

const memberASession: Session = {
  user: {
    id: "member-a",
    displayName: "成员甲",
    mobile: "13800138000",
    admin: false
  },
  csrfToken: "csrf-a",
  expiresAt: "2026-08-01T00:00:00Z"
};

const memberBSession: Session = {
  user: {
    id: "member-b",
    displayName: "成员乙",
    mobile: "13900139000",
    admin: false
  },
  csrfToken: "csrf-b",
  expiresAt: "2026-08-01T00:00:00Z"
};

afterEach(() => {
  memberASetError = undefined;
  memberAUpdateSession = undefined;
  vi.restoreAllMocks();
});

describe("TravelApp 会话缓存边界", () => {
  it("退出和切换成员时隔离上一会话的缓存与迟到回调", async () => {
    vi.spyOn(api, "session").mockResolvedValue(memberASession);
    vi.spyOn(api, "logout").mockResolvedValue(undefined);
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } }
    });
    const previousPackingKey = ["records", "trip-1", "packing-items", "member-a"];
    queryClient.setQueryData(previousPackingKey, { records: [{ recordKey: "私人药品" }] });
    const user = userEvent.setup();

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <TravelApp />
        </MemoryRouter>
      </QueryClientProvider>
    );

    expect(await screen.findByText("当前成员：成员甲")).toBeInTheDocument();
    expect(queryClient.getQueryData(previousPackingKey)).toBeUndefined();

    await user.click(screen.getByRole("button", { name: "制造错误" }));
    expect(screen.getByText("当前错误：成员甲的私密错误")).toBeInTheDocument();
    queryClient.setQueryData(previousPackingKey, { records: [{ recordKey: "私人药品" }] });
    await user.click(screen.getByRole("button", { name: "退出登录" }));
    await screen.findByRole("button", { name: "以成员乙登录" });
    expect(queryClient.getQueryData(previousPackingKey)).toBeUndefined();

    queryClient.setQueryData(previousPackingKey, { records: [{ recordKey: "私人药品" }] });
    await user.click(screen.getByRole("button", { name: "以成员乙登录" }));
    await waitFor(() => expect(screen.getByText("当前成员：成员乙")).toBeInTheDocument());
    expect(queryClient.getQueryData(previousPackingKey)).toBeUndefined();
    expect(screen.getByText("当前错误：无")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "制造错误" }));
    expect(screen.getByText("当前错误：成员乙的私密错误")).toBeInTheDocument();
    act(() => {
      memberASetError?.("成员甲的迟到错误");
      memberAUpdateSession?.({
        ...memberASession,
        user: { ...memberASession.user, displayName: "迟到的成员甲" }
      });
    });
    expect(screen.getByText("当前成员：成员乙")).toBeInTheDocument();
    expect(screen.getByText("当前错误：成员乙的私密错误")).toBeInTheDocument();
  });

  it("会话检查失败时也清除复用 QueryClient 中的旧缓存", async () => {
    vi.spyOn(api, "session").mockRejectedValue(new Error("会话已失效"));
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false }, queries: { retry: false } }
    });
    const previousPackingKey = ["records", "trip-1", "packing-items", "member-a"];
    queryClient.setQueryData(previousPackingKey, { records: [{ recordKey: "私人药品" }] });

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <TravelApp />
        </MemoryRouter>
      </QueryClientProvider>
    );

    await screen.findByRole("button", { name: "以成员乙登录" });
    expect(queryClient.getQueryData(previousPackingKey)).toBeUndefined();
  });
});
