// 地点天气只在在途模式按当前选中地点请求，并显式呈现缓存与失败状态。
import { ArrowClockwise, CloudRain, Sun } from "@phosphor-icons/react";
import { useMemo, useState } from "react";
import type { PlaceData, RecordRow, Trip } from "../../lib/types";
import { formatWeatherFetchedAt } from "./DayWeather";
import { useDayWeather } from "./useDayWeather";

type PlaceWeatherProps = {
  trip: Trip;
  date: string;
  place: RecordRow<PlaceData>;
};

export function PlaceWeather({ trip, date, place }: PlaceWeatherProps) {
  const adcode = String(place.data.weatherAdcode ?? "");
  const locations = useMemo(() => (
    /^\d{6}$/.test(adcode)
      ? [{ adcode, label: place.data.name }]
      : []
  ), [adcode, place.data.name]);
  const weather = useDayWeather(trip.id, locations);
  const [refreshError, setRefreshError] = useState("");

  if (!locations.length) {
    return (
      <section className="place-weather is-unavailable" aria-labelledby="place-weather-title">
        <CloudRain aria-hidden="true" size={20} weight="duotone" />
        <div>
          <h3 id="place-weather-title">天气未配置</h3>
          <p>这个地点还没有 6 位高德行政区代码，暂时不请求天气。</p>
        </div>
      </section>
    );
  }

  const item = weather.items[0];
  const forecast = item?.data?.forecasts.find((entry) => entry.date === date);
  const Icon = forecast?.dayWeather.includes("雨") ? CloudRain : Sun;

  async function refresh() {
    setRefreshError("");
    try {
      await weather.refresh();
    } catch (reason) {
      setRefreshError(localizedWeatherError(reason));
    }
  }

  return (
    <section
      className={`place-weather${item?.data?.freshness === "stale" ? " is-stale" : ""}`}
      aria-labelledby="place-weather-title"
    >
      <header>
        <div>
          <Icon aria-hidden="true" size={22} weight="duotone" />
          <div>
            <h3 id="place-weather-title">地点天气</h3>
            <p>{place.data.name}</p>
          </div>
        </div>
        <button
          type="button"
          aria-label="刷新当前地点天气"
          disabled={weather.refreshing}
          onClick={refresh}
        >
          <ArrowClockwise aria-hidden="true" size={15} />
          <span>{weather.refreshing ? "刷新中" : "刷新"}</span>
        </button>
      </header>
      <div className="place-weather-reading" role="status" aria-live="polite">
        {item?.loading ? (
          <p>正在读取天气…</p>
        ) : item?.error ? (
          <p className="weather-error">{localizedWeatherError(item.error)}</p>
        ) : forecast ? (
          <>
            <strong>{forecast.nightTemp}~{forecast.dayTemp}°C</strong>
            <span>{forecast.dayWeather}</span>
          </>
        ) : (
          <p>高德暂无该日预报。</p>
        )}
      </div>
      {item?.data && (
        <p className="place-weather-meta">
          <time dateTime={item.data.fetchedAt}>
            缓存于 {formatWeatherFetchedAt(item.data.fetchedAt, trip.timezone)}
          </time>
          <span>{item.data.freshness === "stale" ? "缓存已过期" : "缓存有效"}</span>
        </p>
      )}
      {item?.data?.refreshError && (
        <p className="place-weather-warning">最近一次刷新失败，正在显示已有缓存。</p>
      )}
      {refreshError && <p className="place-weather-warning" role="alert">{refreshError}</p>}
    </section>
  );
}

function localizedWeatherError(reason: unknown): string {
  const message = reason instanceof Error ? reason.message : String(reason ?? "");
  return /[\u3400-\u9fff]/u.test(message) ? message : "天气暂时无法读取。";
}
