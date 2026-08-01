// 地图执行台在接近视口后才下载，避免占用“今日”页首屏的脚本与网络预算。
import { MapTrifold } from "@phosphor-icons/react";
import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type Ref
} from "react";
import type { RouteMapViewProps } from "./RouteMapView";

const LazyRouteMapView = lazy(async () => {
  const module = await import("./RouteMapView");
  return { default: module.RouteMapView };
});

export function DeferredRouteMapView(props: RouteMapViewProps) {
  const [ready, setReady] = useState(() => typeof IntersectionObserver === "undefined");
  const placeholderRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (ready) return undefined;
    const element = placeholderRef.current;
    if (!element || typeof IntersectionObserver === "undefined") return undefined;
    const observer = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting)) return;
      setReady(true);
      observer.disconnect();
    }, { rootMargin: "160px 0px", threshold: 0.01 });
    observer.observe(element);
    return () => observer.disconnect();
  }, [ready]);

  if (ready) {
    return (
      <Suspense fallback={<MapPlaceholder loading />}>
        <LazyRouteMapView {...props} />
      </Suspense>
    );
  }
  return <MapPlaceholder ref={placeholderRef} onLoad={() => setReady(true)} />;
}

function MapPlaceholder({
  loading = false,
  onLoad,
  ref
}: {
  loading?: boolean;
  onLoad?: () => void;
  ref?: Ref<HTMLElement>;
}) {
  return (
    <section
      ref={ref}
      className="route-map-deferred"
      role={loading ? "status" : undefined}
      aria-labelledby="deferred-map-title"
      aria-busy={loading || undefined}
    >
      <MapTrifold aria-hidden="true" size={28} weight="duotone" />
      <div>
        <h2 id="deferred-map-title">当天地图</h2>
        <p>{loading
          ? "正在加载地图执行台…"
          : "接近地图区域时再读取高德路线，减少首屏高德脚本和网络开销。"}</p>
      </div>
      {!loading && onLoad && (
        <button className="secondary-button" type="button" onClick={onLoad}>
          立即加载
        </button>
      )}
    </section>
  );
}
