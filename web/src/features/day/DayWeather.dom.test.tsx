// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DayData, PlaceData, RecordRow, Trip, WeatherResponse } from "../../lib/types";
import type { DayPlan } from "./dayModel";
import { DayWeather } from "./DayWeather";

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
  startDate: "2030-04-10",
  endDate: "2030-04-12",
  timezone: "Asia/Shanghai",
  currency: "CNY",
  version: 1
};

const day = record<DayData>("day-a", "days", {
  title: "第一天",
  date: "2030-04-10",
  placeEntries: [
    { placeId: "place-a" },
    { placeId: "place-b" }
  ]
});
const places = [
  record<PlaceData>("place-a", "places", {
    name: "示例甲地",
    weatherAdcode: "110101"
  }),
  record<PlaceData>("place-b", "places", {
    name: "示例乙地",
    weatherAdcode: "110102"
  })
];
const plan: DayPlan = {
  day,
  placesById: new Map(places.map((place) => [place.id, place])),
  entries: places.map((place, index) => ({
    id: `${day.id}:${index}:${place.id}`,
    entry: day.data.placeEntries![index]!,
    place,
    order: index + 1
  })),
  segments: []
};

describe("当天地点天气", () => {
  beforeEach(() => {
    useDayWeatherMock.mockReset();
  });

  it("所有地点都没有当天预报时只显示一条紧凑状态", () => {
    useDayWeatherMock.mockReturnValue({
      items: [
        weatherItem("110101", "示例甲地", weatherResponse({
          adcode: "110101",
          forecasts: [forecast("2030-04-11")]
        })),
        weatherItem("110102", "示例乙地", weatherResponse({
          adcode: "110102",
          forecasts: [forecast("2030-04-12")]
        }))
      ],
      refreshing: false,
      refresh: vi.fn().mockResolvedValue(undefined)
    });

    const { container } = render(
      <DayWeather trip={trip} date="2030-04-10" plan={plan} />
    );

    expect(screen.getByRole("status")).toHaveTextContent("2 个地点暂无");
    expect(container.querySelector(".weather-grid")).not.toBeInTheDocument();
    expect(container.querySelectorAll(".weather-panel article")).toHaveLength(0);
  });

  it("有预报时保留地点卡片并显示缓存时点、有效性和刷新失败", () => {
    useDayWeatherMock.mockReturnValue({
      items: [
        weatherItem("110101", "示例甲地", weatherResponse({
          adcode: "110101",
          fetchedAt: "2030-04-10T06:30:00Z",
          freshness: "stale",
          refreshError: "weather refresh failed; showing the last cached forecast",
          forecasts: [forecast("2030-04-10")]
        })),
        weatherItem("110102", "示例乙地", weatherResponse({
          adcode: "110102",
          forecasts: [forecast("2030-04-11")]
        }))
      ],
      refreshing: false,
      refresh: vi.fn().mockResolvedValue(undefined)
    });

    const { container } = render(
      <DayWeather trip={trip} date="2030-04-10" plan={plan} />
    );

    expect(container.querySelectorAll(".weather-grid article")).toHaveLength(2);
    expect(screen.getByText("22~30°C · 阵雨")).toBeInTheDocument();
    expect(screen.getByText("暂无该日预报")).toBeInTheDocument();
    expect(screen.getByText("4月10日 14:30").closest(".weather-meta"))
      .toHaveTextContent("缓存时间：4月10日 14:30 · 缓存已过期");
    expect(screen.getByText("最近一次刷新失败，正在显示已有缓存。")).toBeInTheDocument();
  });

  it("手动刷新失败时给出中文警报", async () => {
    const user = userEvent.setup();
    useDayWeatherMock.mockReturnValue({
      items: [
        weatherItem("110101", "示例甲地", weatherResponse({
          adcode: "110101",
          forecasts: [forecast("2030-04-10")]
        })),
        weatherItem("110102", "示例乙地", weatherResponse({
          adcode: "110102",
          forecasts: [forecast("2030-04-11")]
        }))
      ],
      refreshing: false,
      refresh: vi.fn().mockRejectedValue(new Error("天气服务繁忙"))
    });
    render(<DayWeather trip={trip} date="2030-04-10" plan={plan} />);

    await user.click(screen.getByRole("button", { name: "刷新" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("天气服务繁忙");
  });
});

function record<T extends Record<string, unknown>>(
  id: string,
  resource: RecordRow["resource"],
  data: T
): RecordRow<T> {
  return {
    id,
    tripId: "trip-a",
    recordKey: id,
    resource,
    position: 0,
    version: 1,
    data,
    createdByMemberId: "member-a",
    updatedByMemberId: "member-a",
    createdAt: "",
    updatedAt: ""
  };
}

function weatherItem(adcode: string, label: string, data: WeatherResponse) {
  return { adcode, label, data, loading: false, error: "" };
}

function weatherResponse(overrides: Partial<WeatherResponse>): WeatherResponse {
  return {
    city: "示例市",
    adcode: "110101",
    province: "示例省",
    reportedAt: "2030-04-10 08:00:00",
    fetchedAt: "2030-04-10T06:00:00Z",
    expiresAt: "2030-04-10T06:30:00Z",
    freshness: "fresh",
    cached: true,
    forecasts: [],
    ...overrides
  };
}

function forecast(date: string): WeatherResponse["forecasts"][number] {
  return {
    date,
    dayWeather: "阵雨",
    nightWeather: "多云",
    dayTemp: "30",
    nightTemp: "22",
    dayWind: "南",
    nightWind: "南",
    dayPower: "≤3",
    nightPower: "≤3"
  };
}
