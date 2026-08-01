import { Crosshair, MagnifyingGlass, MapPin } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type MutableRefObject } from "react";
import { api, mapServiceHost } from "../../lib/api";
import type { PlaceCandidate } from "../../lib/types";

type PlacePickerProps = {
  tripId: string;
  value: Record<string, unknown>;
  onChange: (fields: Record<string, unknown>) => void;
};

type LngLatLike = {
  lng?: number;
  lat?: number;
  getLng?: () => number;
  getLat?: () => number;
};

type AMapMarker = {
  setPosition: (position: [number, number]) => void;
};

type AMapInstance = {
  add: (overlay: AMapMarker) => void;
  destroy: () => void;
  on: (event: "click", listener: (event: { lnglat: LngLatLike }) => void) => void;
  setZoomAndCenter: (zoom: number, center: [number, number]) => void;
};

type AMapNamespace = {
  getConfig: () => { appname: string };
  Map: new (container: HTMLDivElement, options: Record<string, unknown>) => AMapInstance;
  Marker: new (options: Record<string, unknown>) => AMapMarker;
};

export function PlacePicker({ tripId, value, onChange }: PlacePickerProps) {
  const config = useQuery({
    queryKey: ["map-config", tripId],
    queryFn: () => api.mapConfig(tripId)
  });
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<AMapInstance | null>(null);
  const markerRef = useRef<AMapMarker | null>(null);
  const mapAPIRef = useRef<AMapNamespace | null>(null);
  const valueRef = useRef(value);
  const onChangeRef = useRef(onChange);
  const [keyword, setKeyword] = useState("");
  const [results, setResults] = useState<PlaceCandidate[]>([]);
  const [status, setStatus] = useState("搜索地点，或直接点击地图落点");
  const [searching, setSearching] = useState(false);

  useEffect(() => {
    valueRef.current = value;
    onChangeRef.current = onChange;
  }, [onChange, value]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !config.data?.enabled || !config.data.jsapiKey) return;
    let disposed = false;
    window._AMapSecurityConfig = { serviceHost: mapServiceHost() };

    import("@amap/amap-jsapi-loader").then(({ default: AMapLoader }) => AMapLoader.load({
      key: config.data.jsapiKey,
      version: "2.0",
      plugins: []
    })).then((AMap) => {
      if (disposed || !containerRef.current) return;
      const mapAPI = AMap as unknown as AMapNamespace;
      mapAPI.getConfig().appname = "amap-jsapi-skill";
      mapAPIRef.current = mapAPI;
      const initial = coordinates(valueRef.current);
      const map = new mapAPI.Map(containerRef.current, {
        viewMode: "2D",
        zoom: initial ? 15 : 5,
        center: initial ?? [104.1954, 35.8617],
        mapStyle: document.documentElement.dataset.theme === "dark"
          ? "amap://styles/dark"
          : "amap://styles/normal"
      });
      mapRef.current = map;
      if (initial) placeMarker(mapAPI, map, initial, markerRef);
      map.on("click", ({ lnglat }) => {
        const point = normalizeLngLat(lnglat);
        if (point) void selectPoint(point);
      });
    }).catch(() => {
      if (!disposed) setStatus("高德地图加载失败，请稍后重试");
    });

    const selectPoint = async (
      point: [number, number],
      candidate?: Pick<PlaceCandidate, "name" | "address">
    ) => {
      const map = mapRef.current;
      const mapAPI = mapAPIRef.current;
      if (!map || !mapAPI) return;
      placeMarker(mapAPI, map, point, markerRef);
      map.setZoomAndCenter(16, point);
      setStatus("正在确认详细地址…");
      try {
        const detail = await api.reverseGeocode(tripId, point[0], point[1]);
        const address = detail.address || candidate?.address || "";
        onChangeRef.current(buildPlaceSelection({
          name: candidate?.name || String(valueRef.current.name ?? "") || address || "地图选点",
          address,
          longitude: point[0],
          latitude: point[1],
          adcode: detail.adcode,
          region: detail.region
        }));
        setStatus("位置已确认，地址与坐标会随表单保存");
      } catch {
        onChangeRef.current(buildPlaceSelection({
          name: candidate?.name || String(valueRef.current.name ?? "") || "地图选点",
          address: candidate?.address || "",
          longitude: point[0],
          latitude: point[1]
        }));
        setStatus("已记录坐标，详细地址暂未获取");
      }
    };

    return () => {
      disposed = true;
      mapRef.current?.destroy();
      mapRef.current = null;
      markerRef.current = null;
      mapAPIRef.current = null;
    };
  }, [config.data?.enabled, config.data?.jsapiKey, tripId]);

  const search = async () => {
    if (!keyword.trim()) return;
    setSearching(true);
    setStatus("正在搜索高德地点…");
    try {
      const { candidates } = await api.placeSearch(tripId, keyword.trim());
      setResults(candidates);
      setStatus(candidates.length ? "请选择准确地点" : "没有找到匹配地点，请换个关键词");
    } catch (reason) {
      setResults([]);
      setStatus(reason instanceof Error ? reason.message : "地点搜索暂时不可用，请稍后重试");
    } finally {
      setSearching(false);
    }
  };
  const choose = async (candidate: PlaceCandidate) => {
    const map = mapRef.current;
    const mapAPI = mapAPIRef.current;
    const point: [number, number] = [candidate.longitude, candidate.latitude];
    if (map && mapAPI) {
      placeMarker(mapAPI, map, point, markerRef);
      map.setZoomAndCenter(16, point);
    }
    setStatus("正在确认详细地址…");
    try {
      const detail = await api.reverseGeocode(tripId, point[0], point[1]);
      onChangeRef.current(buildPlaceSelection({
        ...candidate,
        address: detail.address || candidate.address,
        adcode: detail.adcode || candidate.adcode,
        region: detail.region || candidate.region
      }));
      setStatus("位置已确认，地址与坐标会随表单保存");
    } catch {
      onChangeRef.current(buildPlaceSelection(candidate));
      setStatus("已记录高德地点与坐标，详细地址暂未获取");
    }
  };
  const current = coordinates(value);

  return (
    <section className="place-picker" aria-labelledby="place-picker-title">
      <div className="place-picker-heading">
        <div>
          <span><MapPin aria-hidden="true" size={17} weight="duotone" /></span>
          <div><b id="place-picker-title">在地图上确认位置</b><small>自动取得地址、经纬度、天气区域和导航入口</small></div>
        </div>
        {current && <strong><Crosshair aria-hidden="true" size={15} />已定位</strong>}
      </div>
      <div className="place-search">
        <label htmlFor="place-keyword" className="sr-only">搜索地点</label>
        <input
          id="place-keyword"
          name="place-keyword"
          type="search"
          autoComplete="off"
          spellCheck={false}
          value={keyword}
          placeholder="例如：城市火车站…"
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              void search();
            }
          }}
        />
        <button className="secondary-button" type="button" disabled={searching || !keyword.trim()} onClick={() => void search()}>
          <MagnifyingGlass aria-hidden="true" size={16} />{searching ? "搜索中…" : "搜索"}
        </button>
      </div>
      {results.length > 0 && (
        <div className="place-results" aria-label="地点搜索结果">
          {results.map((candidate) => (
            <button key={candidate.id} type="button" onClick={() => void choose(candidate)}>
              <b>{candidate.name}</b>
              <small>{candidate.address || "地址由高德地图确认"}</small>
            </button>
          ))}
        </div>
      )}
      {config.isError || config.data?.enabled === false ? (
        <div className="place-map-unavailable" role="status">
          地图预览尚未配置，仍可通过上方搜索确认地点。
        </div>
      ) : (
        <div className="place-picker-map" ref={containerRef} role="region" aria-label="高德地点选取地图" />
      )}
      <div className="place-picker-status" role="status" aria-live="polite">{status}</div>
      {current && (
        <div className="selected-place">
          <b>{String(value.name ?? "已选地点")}</b>
          <span>{String(value.address ?? "地址待确认")}</span>
          <small>{current[0].toFixed(6)}, {current[1].toFixed(6)}</small>
        </div>
      )}
    </section>
  );
}

export function buildPlaceSelection(input: {
  name: string;
  address: string;
  longitude: number;
  latitude: number;
  adcode?: string;
  region?: string;
}): Record<string, unknown> {
  return {
    name: input.name,
    address: input.address,
    longitude: input.longitude,
    latitude: input.latitude,
    ...(input.adcode ? { weatherAdcode: input.adcode } : {}),
    ...(input.region ? { region: input.region } : {}),
    navigationURL: amapNavigationURL(input.longitude, input.latitude, input.name)
  };
}

export function amapNavigationURL(longitude: number, latitude: number, name: string): string {
  const query = new URLSearchParams({
    to: `${longitude},${latitude},${name}`,
    mode: "car",
    policy: "1",
    src: "my-travel",
    coordinate: "gaode",
    callnative: "1"
  });
  return `https://uri.amap.com/navigation?${query.toString()}`;
}

function coordinates(value: Record<string, unknown>): [number, number] | undefined {
  if (value.longitude === "" || value.latitude === "") return undefined;
  const longitude = Number(value.longitude);
  const latitude = Number(value.latitude);
  return Number.isFinite(longitude) && Number.isFinite(latitude)
    ? [longitude, latitude]
    : undefined;
}

function normalizeLngLat(value: LngLatLike): [number, number] | undefined {
  const longitude = value.getLng?.() ?? value.lng;
  const latitude = value.getLat?.() ?? value.lat;
  return Number.isFinite(longitude) && Number.isFinite(latitude)
    ? [Number(longitude), Number(latitude)]
    : undefined;
}

function placeMarker(
  AMap: AMapNamespace,
  map: AMapInstance,
  point: [number, number],
  markerRef: MutableRefObject<AMapMarker | null>
) {
  if (markerRef.current) {
    markerRef.current.setPosition(point);
    return;
  }
  const marker = new AMap.Marker({ position: point, anchor: "bottom-center" });
  map.add(marker);
  markerRef.current = marker;
}
