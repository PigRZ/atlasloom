// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConfirmProvider } from "../../components/ConfirmDialog";
import { api } from "../../lib/api";
import type { SnapshotSummary, Trip } from "../../lib/types";
import { TripsDashboard } from "./TripsDashboard";

const trip: Trip = {
  id: "trip-a",
  title: "山海漫游示例",
  summary: "四天示例行程，优先关注天气变化。",
  startDate: "2030-04-10",
  endDate: "2030-04-13",
  timezone: "Asia/Shanghai",
  currency: "CNY",
  version: 3,
  role: "owner"
};

const snapshot: SnapshotSummary = {
  id: "snapshot-a",
  revision: 4,
  sha256: "a".repeat(64),
  contentChecksum: "b".repeat(64),
  mirrorState: "mirrored",
  createdAt: "2030-04-05T00:00:00Z"
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("旅行说明编辑", () => {
  it("在卡片展示说明，并在恢复原值后清除未保存状态", async () => {
    const user = userEvent.setup();
    renderDashboard();

    expect(await screen.findByText(trip.summary)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "调整" }));
    const summary = screen.getByRole("textbox", { name: /旅行说明/ });
    const submit = screen.getByRole("button", { name: "保存旅行" });
    expect(summary).toHaveValue(trip.summary);
    expect(submit).toBeDisabled();

    await user.clear(summary);
    expect(screen.getByRole("status")).toHaveTextContent("有未保存的修改");
    expect(submit).toBeEnabled();
    await user.type(summary, trip.summary);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(submit).toBeDisabled();
  });

  it("提交旅行说明和当前可编辑字段，不回传只读 Trip 字段", async () => {
    const user = userEvent.setup();
    const updateTrip = vi.spyOn(api, "updateTrip").mockResolvedValue({
      trip: { ...trip, summary: "把日落观景调整为天气合适时再执行", version: 4 },
      snapshot
    });
    renderDashboard();

    await user.click(await screen.findByRole("button", { name: "调整" }));
    const summary = screen.getByRole("textbox", { name: /旅行说明/ });
    await user.clear(summary);
    await user.type(summary, "把日落观景调整为天气合适时再执行");
    await user.click(screen.getByRole("button", { name: "保存旅行" }));

    await waitFor(() => expect(updateTrip).toHaveBeenCalledWith(trip, {
      title: trip.title,
      summary: "把日落观景调整为天气合适时再执行",
      startDate: trip.startDate,
      endDate: trip.endDate
    }));
    await waitFor(() => expect(
      screen.queryByRole("dialog", { name: "调整旅行" })
    ).not.toBeInTheDocument());
  });
});

function renderDashboard() {
  vi.spyOn(api, "overview").mockResolvedValue({
    counts: { days: 4, places: 8 }
  });
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false }
    }
  });
  return render(
    <QueryClientProvider client={client}>
      <ConfirmProvider>
        <TripsDashboard
          trips={[trip]}
          admin={false}
          onOpenTrip={vi.fn()}
          onCreatedTrip={vi.fn()}
          setError={vi.fn()}
          onSnapshot={vi.fn()}
          onDirtyChange={vi.fn()}
        />
      </ConfirmProvider>
    </QueryClientProvider>
  );
}
