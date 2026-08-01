// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  createMemoryRouter,
  RouterProvider,
  useLocation,
  useNavigate
} from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConfirmProvider } from "../../components/ConfirmDialog";
import { api } from "../../lib/api";
import type { Session, Trip } from "../../lib/types";
import { TravelShell } from "./TravelShell";

vi.mock("../day/DayConsole", () => ({
  DayConsole: ({ onDirtyChange }: { onDirtyChange: (dirty: boolean) => void }) => (
    <button type="button" onClick={() => onDirtyChange(true)}>
      制造未保存修改
    </button>
  )
}));

const trip: Trip = {
  id: "trip-a",
  title: "虚构旅行",
  summary: "用于验证旅行导航的示例说明",
  startDate: "2026-09-25",
  endDate: "2026-09-28",
  timezone: "Asia/Shanghai",
  currency: "CNY",
  version: 1,
  role: "owner"
};

const session: Session = {
  user: {
    id: "member-a",
    displayName: "示例成员",
    mobile: "13800138000",
    admin: false
  },
  csrfToken: "csrf",
  expiresAt: "2026-09-30T00:00:00Z"
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TravelShell 导航安全", () => {
  it("失效深链不会静默切换到第一场旅行", async () => {
    vi.spyOn(api, "trips").mockResolvedValue({ trips: [trip] });
    const navigateTo = vi.fn();

    renderShell({
      route: { page: "today", tripId: "missing-trip", date: "2026-09-25" },
      navigateTo
    });

    expect(await screen.findByRole("heading", {
      name: "旅行不存在或当前账号无权访问"
    })).toBeInTheDocument();
    expect(screen.getByText(/系统没有替你切换到其他旅行/)).toBeInTheDocument();
    expect(navigateTo).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "返回所有旅行" }));
    expect(navigateTo).toHaveBeenCalledWith("/app/trips");
  });

  it("站内链接和浏览器历史共用未保存修改确认", async () => {
    vi.spyOn(api, "trips").mockResolvedValue({ trips: [trip] });
    const user = userEvent.setup();

    renderShell({
      route: { page: "today", tripId: trip.id, date: "2026-09-25" }
    });
    await user.click(await screen.findByRole("button", {
      name: "制造未保存修改"
    }));
    await user.click(screen.getAllByRole("link", { name: "所有旅行" })[0]!);

    expect(await screen.findByRole("alertdialog")).toHaveTextContent(
      "当前表单有未保存的修改"
    );
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/app/trips/trip-a/day/2026-09-25"
    );

    await user.click(screen.getAllByRole("link", { name: "所有旅行" })[0]!);
    await user.click(screen.getByRole("button", { name: "放弃并离开" }));
    await waitFor(() => expect(screen.getByTestId("location")).toHaveTextContent(
      "/app/trips"
    ));
  });

  it("移动更多菜单打开后接管焦点并在关闭时恢复", async () => {
    vi.spyOn(api, "trips").mockResolvedValue({ trips: [trip] });
    const user = userEvent.setup();

    renderShell({
      route: { page: "today", tripId: trip.id, date: "2026-09-25" }
    });
    const trigger = await screen.findByRole("button", { name: "更多" });
    await user.click(trigger);
    const menu = screen.getByRole("navigation", { name: "更多页面" });
    const first = within(menu).getByRole("link", { name: "地点" });
    const last = within(menu).getByRole("link", { name: "操作记录" });
    await waitFor(() => expect(first).toHaveFocus());

    await user.tab({ shift: true });
    expect(last).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });
});

function renderShell({
  route,
  navigateTo
}: {
  route: {
    page: "today";
    tripId: string;
    date: string;
  };
  navigateTo?: (path: string, replace?: boolean) => void;
}) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false }
    }
  });

  function Harness() {
    const navigate = useNavigate();
    const location = useLocation();
    return (
      <>
        <TravelShell
          session={session}
          route={route}
          navigateTo={navigateTo ?? ((path, replace = false) => navigate(path, { replace }))}
          error=""
          setError={vi.fn()}
          onLogout={vi.fn()}
          onSessionChange={vi.fn()}
        />
        <output data-testid="location">{location.pathname}</output>
      </>
    );
  }
  const router = createMemoryRouter(
    [{ path: "*", element: <Harness /> }],
    { initialEntries: ["/app/trips/trip-a/day/2026-09-25"] }
  );

  return render(
    <QueryClientProvider client={client}>
      <ConfirmProvider>
        <RouterProvider router={router} />
      </ConfirmProvider>
    </QueryClientProvider>
  );
}
