// 高德地图只绘制当前日：驾车、步行和骑行请求导航路线，其余方式显示为示意关系。
import { useEffect, useRef, useState } from "react";
import { api, mapServiceHost } from "../../lib/api";
import type { MapConfig } from "../../lib/types";
import {
  hasCoordinate,
  roadLegs,
  schematicLegs,
  type RouteDay,
  type RouteLeg,
  type RoutePoint
} from "./routeMapModel";

export type RouteMapSummary = {
  distance: number;
  duration: number;
  successful: number;
  total: number;
  schematic: number;
  pending: boolean;
};

type AmapRouteMapProps = {
  tripId: string;
  config: MapConfig;
  day: RouteDay;
  selectedStopId: string;
  onSelectStop: (stopId: string) => void;
  onSummary: (summary: RouteMapSummary) => void;
};

type AMapOverlay = object;
type AMapInstance = {
  add: (overlay: AMapOverlay | AMapOverlay[]) => void;
  destroy: () => void;
  setCenter: (
    position: [number, number],
    immediately?: boolean,
    duration?: number
  ) => void;
  setFitView: (
    overlays?: AMapOverlay[],
    immediately?: boolean,
    avoid?: [number, number, number, number],
    maxZoom?: number
  ) => void;
  setMapStyle: (style: string) => void;
};
type MarkerInstance = AMapOverlay & {
  on: (event: string, listener: () => void) => void;
};
type InfoWindowInstance = {
  setContent: (content: HTMLElement) => void;
  open: (map: AMapInstance, position: [number, number]) => void;
};
type AMapNamespace = {
  getConfig: () => { appname: string };
  Map: new (container: HTMLDivElement, options: Record<string, unknown>) => AMapInstance;
  Marker: new (options: Record<string, unknown>) => MarkerInstance;
  InfoWindow: new (options: Record<string, unknown>) => InfoWindowInstance;
  Polyline: new (options: Record<string, unknown>) => AMapOverlay;
};

declare global {
  interface Window {
    _AMapSecurityConfig?: { serviceHost: string };
  }
}

export function AmapRouteMap({
  tripId,
  config,
  day,
  selectedStopId,
  onSelectStop,
  onSummary
}: AmapRouteMapProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<AMapInstance | null>(null);
  const pointPositionsRef = useRef(new Map<string, [number, number]>());
  const selectedStopRef = useRef(selectedStopId);
  const [status, setStatus] = useState("正在读取当日道路…");

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !config.enabled || !config.jsapiKey) return;

    let disposed = false;
    let map: AMapInstance | null = null;
    let themeObserver: MutationObserver | null = null;
    const roads = roadLegs(day);
    const schematic = schematicLegs(day);
    const points = day.points.filter(hasCoordinate);

    window._AMapSecurityConfig = { serviceHost: mapServiceHost() };
    onSummary(emptySummary(schematic.length, roads.length));
    setStatus(roads.length ? "正在读取当日道路…" : routeStatus(0, 0, schematic.length, points.length));

    import("@amap/amap-jsapi-loader").then(({ default: AMapLoader }) => AMapLoader.load({
      key: config.jsapiKey,
      version: "2.0",
      plugins: []
    })).then((AMap) => {
      AMap.getConfig().appname = 'amap-jsapi-skill';
      const mapAPI = AMap as unknown as AMapNamespace;
      if (disposed || !containerRef.current) return;

      map = new mapAPI.Map(containerRef.current, {
        viewMode: "3D",
        zoom: 8,
        pitch: 0,
        mapStyle: mapStyle()
      });
      mapRef.current = map;
      pointPositionsRef.current = new Map(points.map((point) => [
        point.id,
        [point.longitude, point.latitude]
      ]));
      const overlays: AMapOverlay[] = [];
      const infoWindow = new mapAPI.InfoWindow({
        offset: [0, -28],
        closeWhenClickMap: true
      });

      points.forEach((point) => {
        const marker = new mapAPI.Marker({
          position: [point.longitude, point.latitude],
          content: markerElement(point, point.id === selectedStopRef.current),
          anchor: "bottom-center",
          title: point.name
        });
        marker.on("click", () => {
          onSelectStop(point.id);
          infoWindow.setContent(infoWindowElement(point));
          infoWindow.open(map!, [point.longitude, point.latitude]);
        });
        map!.add(marker);
        overlays.push(marker);
      });

      schematic.forEach((leg) => {
        const polyline = new mapAPI.Polyline({
          path: [coordinateOf(leg.from), coordinateOf(leg.to)].filter(isCoordinate),
          strokeColor: "#8b735a",
          strokeWeight: 4,
          strokeOpacity: 0.78,
          strokeStyle: "dashed",
          strokeDasharray: [8, 8],
          zIndex: 40
        });
        map!.add(polyline);
        overlays.push(polyline);
      });

      themeObserver = new MutationObserver(() => map?.setMapStyle(mapStyle()));
      themeObserver.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ["data-theme"]
      });

      return Promise.all(roads.map((leg) => calculateRoad(tripId, leg))).then((routes) => {
        if (disposed || !map) return;
        let distance = 0;
        let duration = 0;
        let successful = 0;
        routes.forEach((route) => {
          if (!route) return;
          successful += 1;
          distance += route.distance;
          duration += route.duration;
          const style = routeStyle(route.mode);
          const polyline = new mapAPI.Polyline({
            path: route.path,
            isOutline: true,
            outlineColor: "rgba(255, 255, 255, 0.82)",
            borderWeight: 2,
            strokeColor: style.color,
            strokeWeight: style.weight,
            strokeOpacity: 0.94,
            strokeStyle: style.strokeStyle,
            strokeDasharray: style.strokeDasharray,
            lineJoin: "round",
            lineCap: "round",
            showDir: true,
            zIndex: 48
          });
          map!.add(polyline);
          overlays.push(polyline);
        });
        onSummary({
          distance,
          duration,
          successful,
          total: roads.length,
          schematic: schematic.length,
          pending: false
        });
        setStatus(routeStatus(successful, roads.length, schematic.length, points.length));
        if (overlays.length) map.setFitView(overlays, false, [72, 52, 82, 52], 13);
        const selectedPosition = pointPositionsRef.current.get(selectedStopRef.current);
        if (selectedPosition) map.setCenter(selectedPosition, false, 180);
      });
    }).catch(() => {
      if (!disposed) setStatus("高德地图暂未加载；当天清单和外部导航仍可使用");
    });

    return () => {
      disposed = true;
      themeObserver?.disconnect();
      mapRef.current = null;
      pointPositionsRef.current.clear();
      map?.destroy();
    };
  }, [config.enabled, config.jsapiKey, day, onSelectStop, onSummary, tripId]);

  useEffect(() => {
    selectedStopRef.current = selectedStopId;
    containerRef.current?.querySelectorAll<HTMLElement>(".amap-route-marker").forEach((marker) => {
      marker.classList.toggle("is-selected", marker.dataset.stopId === selectedStopId);
    });
    const selectedPosition = pointPositionsRef.current.get(selectedStopId);
    if (selectedPosition) mapRef.current?.setCenter(selectedPosition, false, 180);
  }, [selectedStopId]);

  return (
    <div className="route-map-stage">
      <div
        className="route-map-canvas"
        ref={containerRef}
        role="region"
        aria-label={`${day.date} 高德地图`}
      />
      <div className="route-map-status" role="status" aria-live="polite">
        <span aria-hidden="true" />
        {status}
      </div>
    </div>
  );
}

async function calculateRoad(tripId: string, leg: RouteLeg) {
  const origin = coordinateOf(leg.from);
  const destination = coordinateOf(leg.to);
  const mode = drawableMode(leg.mode);
  if (!origin || !destination || !mode) return null;
  try {
    const plan = await api.routePlan(tripId, mode, origin, destination);
    return {
      distance: Number(plan.distanceMeters) || 0,
      duration: Number(plan.durationSeconds) || 0,
      path: plan.path,
      mode
    };
  } catch (reason) {
    console.warn(
      `高德导航路线规划失败：方式=${leg.mode}，原因=${
        reason instanceof Error ? reason.message : "未知"
      }`
    );
    return null;
  }
}

function coordinateOf(point: RoutePoint | undefined): [number, number] | null {
  return point && hasCoordinate(point) ? [point.longitude, point.latitude] : null;
}

function drawableMode(
  mode: RouteLeg["mode"]
): "drive" | "taxi" | "rideshare" | "walk" | "bicycle" | null {
  return ["drive", "taxi", "rideshare", "walk", "bicycle"].includes(mode)
    ? mode as "drive" | "taxi" | "rideshare" | "walk" | "bicycle"
    : null;
}

function isCoordinate(value: [number, number] | null): value is [number, number] {
  return value !== null;
}

function markerElement(point: RoutePoint, selected: boolean): HTMLButtonElement {
  const marker = document.createElement("button");
  marker.type = "button";
  marker.className = [
    "amap-route-marker",
    selected ? "is-selected" : "",
  ].filter(Boolean).join(" ");
  marker.dataset.stopId = point.id;
  marker.setAttribute("aria-label", `${point.order}. ${point.name}`);
  const number = document.createElement("span");
  number.textContent = String(point.order);
  const label = document.createElement("b");
  label.textContent = point.name;
  marker.append(number, label);
  return marker;
}

function infoWindowElement(point: RoutePoint): HTMLDivElement {
  const content = document.createElement("div");
  content.className = "amap-route-info";
  const title = document.createElement("b");
  title.textContent = `${point.order}. ${point.name}`;
  const detail = document.createElement("span");
  detail.textContent = [point.arrivalTime, point.additionalInfo].filter(Boolean).join(" · ");
  content.append(title, detail);
  return content;
}

function routeStyle(mode: RouteLeg["mode"]): {
  color: string;
  weight: number;
  strokeStyle: "solid" | "dashed";
  strokeDasharray?: [number, number];
} {
  if (mode === "walk") {
    return { color: "#4f7f6e", weight: 5, strokeStyle: "dashed", strokeDasharray: [5, 7] };
  }
  if (mode === "bicycle") {
    return { color: "#34745e", weight: 5, strokeStyle: "solid" };
  }
  return { color: "#1f604b", weight: 7, strokeStyle: "solid" };
}

function emptySummary(schematic = 0, total = 0): RouteMapSummary {
  return {
    distance: 0,
    duration: 0,
    successful: 0,
    total,
    schematic,
    pending: total > 0
  };
}

function mapStyle(): string {
  return document.documentElement.dataset.theme === "dark"
    ? "amap://styles/darkblue"
    : "amap://styles/whitesmoke";
}

function routeStatus(successful: number, total: number, schematic: number, points: number): string {
  const schematicText = schematic ? `；${schematic} 段不支持导航的交通为示意线` : "";
  if (!total) return points ? `地点已标记${schematicText}` : "当天没有可定位的地点";
  if (successful === total) return `已绘制 ${successful} 段导航路线${schematicText}`;
  if (successful) return `已绘制 ${successful}/${total} 段导航路线${schematicText}`;
  return `导航服务暂未返回路线，未用直线代替${schematicText}`;
}
