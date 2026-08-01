// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PlaceData, RecordRow, Trip, WeatherResponse } from "../../lib/types";
import { PlaceWeather } from "./PlaceWeather";

const { useDayWeatherMock } = vi.hoisted(() => ({
  useDayWeatherMock: vi.fn()
}));

vi.mock("./useDayWeather", () => ({
  useDayWeather: useDayWeatherMock
}));

const trip: Trip = {
  id: "trip-a",
  title: "旅行",
  summary: "",
  startDate: "2026-09-25",
  endDate: "2026-09-27",
  timezone: "Asia/Shanghai",
  currency: "CNY",
  version: 1
};

describe("在途地点天气", () => {
  beforeEach(() => useDayWeatherMock.mockReset());

  it("缺少高德行政区代码时明确说明且不伪造天气", () => {
    useDayWeatherMock.mockReturnValue({ items: [], refreshing: false, refresh: vi.fn() });
    render(<PlaceWeather trip={trip} date="2026-09-25" place={place({})} />);

    expect(screen.getByRole("heading", { name: "天气未配置" })).toBeInTheDocument();
    expect(useDayWeatherMock).toHaveBeenCalledWith(trip.id, []);
  });

  it("只展示选中地点预报和缓存新鲜度", () => {
    useDayWeatherMock.mockReturnValue({
      items: [{
        adcode: "110101",
        label: "示例游客中心",
        loading: false,
        error: "",
        data: weatherResponse()
      }],
      refreshing: false,
      refresh: vi.fn()
    });
    render(<PlaceWeather trip={trip} date="2026-09-25" place={place({ weatherAdcode: "110101" })} />);

    expect(screen.getByText("18~26°C")).toBeInTheDocument();
    expect(screen.getByText("多云")).toBeInTheDocument();
    expect(screen.getByText("缓存有效")).toBeInTheDocument();
    expect(useDayWeatherMock).toHaveBeenCalledWith(trip.id, [{
      adcode: "110101",
      label: "示例游客中心"
    }]);
  });

  it("手动刷新失败时给出中文警报", async () => {
    const user = userEvent.setup();
    useDayWeatherMock.mockReturnValue({
      items: [{
        adcode: "110101",
        label: "示例游客中心",
        loading: false,
        error: "",
        data: weatherResponse()
      }],
      refreshing: false,
      refresh: vi.fn().mockRejectedValue(new Error("天气服务繁忙"))
    });
    render(<PlaceWeather trip={trip} date="2026-09-25" place={place({ weatherAdcode: "110101" })} />);

    await user.click(screen.getByRole("button", { name: "刷新当前地点天气" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("天气服务繁忙");
  });
});

function place(overrides: Partial<PlaceData>): RecordRow<PlaceData> {
  return {
    id: "place-a",
    tripId: trip.id,
    recordKey: "place-a",
    resource: "places",
    position: 0,
    version: 1,
    data: { name: "示例游客中心", ...overrides },
    createdByMemberId: "member-a",
    updatedByMemberId: "member-a",
    createdAt: "",
    updatedAt: ""
  };
}

function weatherResponse(): WeatherResponse {
  return {
    city: "示例市",
    adcode: "110101",
    province: "示例省",
    reportedAt: "2026-09-25T06:00:00Z",
    fetchedAt: "2026-09-25T06:30:00Z",
    expiresAt: "2026-09-25T07:00:00Z",
    freshness: "fresh",
    cached: true,
    forecasts: [{
      date: "2026-09-25",
      dayWeather: "多云",
      nightWeather: "晴",
      dayTemp: "26",
      nightTemp: "18",
      dayWind: "南",
      nightWind: "南",
      dayPower: "≤3",
      nightPower: "≤3"
    }]
  };
}
