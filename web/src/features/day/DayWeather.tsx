// 当天天气只负责读取地点预报与呈现缓存状态，不改变 Trip 资料。
import { CloudRain, Sun } from "@phosphor-icons/react";
import { useMemo, useState } from "react";
import { formatDayDate } from "../../lib/formatters";
import type { Trip } from "../../lib/types";
import { type DayPlan, weatherLocations } from "./dayModel";
import { useDayWeather } from "./useDayWeather";

type DayWeatherProps = {
  trip: Trip;
  date: string;
  plan: DayPlan;
};

export function DayWeather({ trip, date, plan }: DayWeatherProps) {
  const locations = useMemo(() => weatherLocations(plan), [plan]);
  const weather = useDayWeather(trip.id, locations);
  const [refreshError, setRefreshError] = useState("");

  if (!locations.length) return null;

  const displayItems = weather.items.map((item) => ({
    ...item,
    forecast: item.data?.forecasts.find((entry) => entry.date === date)
  }));
  const forecastCount = displayItems.filter((item) => item.forecast).length;
  const loadingCount = displayItems.filter((item) => item.loading).length;
  const readErrorCount = displayItems.filter((item) => item.error).length;
  const cachedRefreshErrorCount = displayItems.filter((item) => item.data?.refreshError).length;

  function refresh() {
    setRefreshError("");
    weather.refresh().catch((reason) => {
      setRefreshError(localizedRefreshError(reason));
    });
  }

  return (
    <section className="weather-panel" aria-labelledby="weather-title">
      <header>
        <div>
          <CloudRain aria-hidden="true" size={20} />
          <div>
            <h2 id="weather-title">地点天气</h2>
            <p>{formatDayDate(date)} · 高德预报</p>
          </div>
        </div>
        <button
          className="secondary-button"
          type="button"
          disabled={weather.refreshing}
          onClick={refresh}
        >
          {weather.refreshing ? "刷新中…" : "刷新"}
        </button>
      </header>

      {forecastCount === 0 ? (
        <div className="weather-compact-state" role="status" aria-live="polite">
          <CloudRain aria-hidden="true" size={20} weight="duotone" />
          <div>
            <p>
              {compactWeatherMessage(
                locations.length,
                date,
                loadingCount,
                readErrorCount
              )}
            </p>
            {cachedRefreshErrorCount > 0 && (
              <small>
                {cachedRefreshErrorCount} 个地点最近刷新失败，缓存中也没有该日预报。
              </small>
            )}
          </div>
        </div>
      ) : (
        <div className="weather-grid">
          {displayItems.map((item) => {
            const Icon = item.forecast?.dayWeather.includes("雨") ? CloudRain : Sun;
            return (
              <article
                className={item.data?.freshness === "stale" ? "is-stale" : undefined}
                key={item.adcode}
              >
                <Icon aria-hidden="true" size={22} weight="duotone" />
                <div>
                  <b>{item.label}</b>
                  {item.loading ? (
                    <p>读取中…</p>
                  ) : item.error ? (
                    <p className="weather-error">{localizedReadError(item.error)}</p>
                  ) : item.forecast ? (
                    <strong>
                      {item.forecast.nightTemp}~{item.forecast.dayTemp}°C · {item.forecast.dayWeather}
                    </strong>
                  ) : (
                    <p>暂无该日预报</p>
                  )}
                  {item.data && (
                    <small className="weather-meta">
                      缓存时间：
                      <time dateTime={item.data.fetchedAt}>
                        {formatWeatherFetchedAt(item.data.fetchedAt, trip.timezone)}
                      </time>
                      {" · "}
                      {item.data.freshness === "stale" ? "缓存已过期" : "缓存有效"}
                    </small>
                  )}
                  {item.data?.refreshError && (
                    <small className="weather-cache-warning">
                      最近一次刷新失败，正在显示已有缓存。
                    </small>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      )}

      {refreshError && (
        <p className="weather-refresh-error" role="alert">{refreshError}</p>
      )}
    </section>
  );
}

export function formatWeatherFetchedAt(value: string, timezone: string): string {
  const instant = new Date(value);
  if (Number.isNaN(instant.getTime())) return "未知";
  try {
    return new Intl.DateTimeFormat("zh-CN", {
      month: "long",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
      timeZone: timezone || "Asia/Shanghai"
    }).format(instant);
  } catch {
    return new Intl.DateTimeFormat("zh-CN", {
      month: "long",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
      timeZone: "Asia/Shanghai"
    }).format(instant);
  }
}

function compactWeatherMessage(
  locationCount: number,
  date: string,
  loadingCount: number,
  readErrorCount: number
): string {
  if (loadingCount > 0) return `正在读取 ${locationCount} 个地点的天气…`;
  if (readErrorCount === locationCount) return `${locationCount} 个地点的天气暂时无法读取。`;
  const unavailable = `${locationCount} 个地点暂无 ${formatDayDate(date)} 的预报。`;
  return readErrorCount > 0
    ? `${unavailable} 其中 ${readErrorCount} 个地点读取失败。`
    : unavailable;
}

function localizedReadError(message: string): string {
  return /[\u3400-\u9fff]/u.test(message) ? message : "天气暂时无法读取。";
}

function localizedRefreshError(reason: unknown): string {
  if (reason instanceof Error && /[\u3400-\u9fff]/u.test(reason.message)) {
    return reason.message;
  }
  return "天气刷新失败，请稍后重试。";
}
