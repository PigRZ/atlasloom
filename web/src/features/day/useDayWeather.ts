// 单日天气钩子统一高德缓存读取、显式刷新与逐地点失败状态。
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { api } from "../../lib/api";
import type { WeatherResponse } from "../../lib/types";

export type WeatherLocation = { adcode: string; label: string };
export type DayWeatherItem = WeatherLocation & {
  data?: WeatherResponse;
  loading: boolean;
  error: string;
};

export function useDayWeather(tripId: string, locations: WeatherLocation[]) {
  const queryClient = useQueryClient();
  const [refreshing, setRefreshing] = useState(false);
  const results = useQueries({
    queries: locations.map((location) => ({
      queryKey: ["weather", tripId, location.adcode],
      queryFn: () => api.weather(tripId, location.adcode),
      staleTime: 5 * 60_000
    }))
  });
  const items: DayWeatherItem[] = locations.map((location, index) => {
    const result = results[index];
    return {
      ...location,
      data: result?.data,
      loading: result?.isLoading === true,
      error: result?.error instanceof Error ? result.error.message : ""
    };
  });
  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await Promise.all(locations.map(async (location) => {
        const data = await api.weather(tripId, location.adcode, true);
        queryClient.setQueryData<WeatherResponse>(
          ["weather", tripId, location.adcode],
          data
        );
      }));
    } finally {
      setRefreshing(false);
    }
  }, [locations, queryClient, tripId]);

  return { items, refreshing, refresh };
}
