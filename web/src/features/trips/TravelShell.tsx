// TravelShell 负责旅行切换、URL 导航、移动端一级入口与页面分发。
import { AirplaneTilt, Database, SidebarSimple, SignOut } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useBeforeUnload, useBlocker } from "react-router";
import {
  desktopPrimaryPages,
  mobileMorePages,
  mobilePrimaryPages,
  pageLabel,
  pagePath,
  pagePathForContext,
  type AppRoute,
  type Page
} from "../../app/navigation";
import { DataError, EmptyTrips, PageSkeleton } from "../../components/PageStates";
import { useConfirm } from "../../components/ConfirmDialog";
import { ThemeToggle } from "../../components/ThemeToggle";
import { api, snapshotWarningAfterWrite } from "../../lib/api";
import { formatDateRange } from "../../lib/formatters";
import type { PageResource, Session, SnapshotSummary, Trip } from "../../lib/types";
import { DayConsole } from "../day/DayConsole";
import { ItineraryPage } from "../day/ItineraryPage";
import { MapPage } from "../day/MapPage";
import { LibraryPage } from "../library/LibraryPage";
import { ResourceView } from "../records/ResourceView";
import { AuditLogPage } from "../records/AuditLogPage";
import { MorePage } from "./MorePage";
import { TripsDashboard } from "./TripsDashboard";

type TravelShellProps = {
  session: Session;
  route: AppRoute;
  navigateTo: (path: string, replace?: boolean) => void;
  error: string;
  setError: (value: string) => void;
  onLogout: () => void;
  onSessionChange: (session: Session) => void;
};

export function TravelShell({
  session,
  route,
  navigateTo,
  error,
  setError,
  onLogout,
  onSessionChange
}: TravelShellProps) {
  const trips = useQuery({ queryKey: ["trips"], queryFn: api.trips });
  const confirm = useConfirm();
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [snapshotWarning, setSnapshotWarning] = useState("");
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [mobileMoreContext, setMobileMoreContext] = useState("");
  const mobileMoreButtonRef = useRef<HTMLButtonElement>(null);
  const mobileMoreMenuRef = useRef<HTMLElement>(null);
  const mobileMoreContextKey = `${route.tripId}:${route.page}`;
  const mobileMoreOpen = mobileMoreContext === mobileMoreContextKey;
  const requestedTrip = trips.data?.trips.find((trip) => trip.id === route.tripId);
  const activeTrip = route.tripId
    ? requestedTrip ?? null
    : trips.data?.trips[0] ?? null;
  const missingRequestedTrip = trips.isSuccess
    && route.page !== "trips"
    && route.page !== "library"
    && Boolean(route.tripId)
    && !requestedTrip;
  const bypassNextBlockRef = useRef(false);
  const shouldBlock = useCallback(
    () => hasUnsavedChanges && !bypassNextBlockRef.current,
    [hasUnsavedChanges]
  );
  const blocker = useBlocker(shouldBlock);
  const unsavedChangesRef = useRef(hasUnsavedChanges);
  const resolvingBlocker = useRef(false);

  useEffect(() => {
    unsavedChangesRef.current = hasUnsavedChanges;
  }, [hasUnsavedChanges]);

  useEffect(() => {
    if (!trips.isSuccess || route.page === "trips" || route.page === "library") return;
    if (route.tripId) return;
    if (!activeTrip) {
      navigateTo(pagePath("", "trips"), true);
      return;
    }
    navigateTo(pagePath(activeTrip.id, "today"), true);
  }, [activeTrip, navigateTo, route.page, route.tripId, trips.isSuccess]);
  useEffect(() => {
    bypassNextBlockRef.current = false;
  }, [route.date, route.page, route.tripId]);
  useEffect(() => {
    if (!mobileMoreOpen) return;
    const focusFrame = window.requestAnimationFrame(() => {
      mobileMoreMenuRef.current?.querySelector<HTMLElement>(".mobile-more-item")?.focus();
    });
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setMobileMoreContext("");
        mobileMoreButtonRef.current?.focus();
        return;
      }
      if (event.key === "Tab") {
        const items = [...(mobileMoreMenuRef.current?.querySelectorAll<HTMLElement>(
          ".mobile-more-item"
        ) ?? [])];
        const first = items[0];
        const last = items.at(-1);
        if (!first || !last) return;
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.cancelAnimationFrame(focusFrame);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [mobileMoreOpen]);

  const confirmLeave = useCallback(() => (
    !hasUnsavedChanges
      ? Promise.resolve(true)
      : confirm({
        title: "离开并放弃修改？",
        message: "当前表单有未保存的修改，离开后不会保留。",
        confirmLabel: "放弃并离开",
        tone: "danger"
      })
  ), [confirm, hasUnsavedChanges]);
  useBeforeUnload(useCallback((event) => {
    if (!hasUnsavedChanges) return;
    event.preventDefault();
    event.returnValue = "";
  }, [hasUnsavedChanges]));
  useEffect(() => {
    if (
      blocker.state !== "blocked"
      || resolvingBlocker.current
      || !hasUnsavedChanges
    ) return;
    const blockedLocationKey = blocker.location.key;
    resolvingBlocker.current = true;
    void confirmLeave().then((leave) => {
      resolvingBlocker.current = false;
      if (!unsavedChangesRef.current) return;
      if (blocker.state !== "blocked" || blocker.location.key !== blockedLocationKey) return;
      if (leave) blocker.proceed();
      else blocker.reset();
    });
  }, [blocker, confirmLeave, hasUnsavedChanges]);

  const guardedNavigate = (path: string, replace = false) => navigateTo(path, replace);
  const navigateAfterSuccessfulWrite = (path: string) => {
    bypassNextBlockRef.current = true;
    setHasUnsavedChanges(false);
    navigateTo(path);
  };
  const linkFor = (page: Page) => activeTrip
    ? pagePathForContext(activeTrip.id, page, route.date)
    : "#";
  const mobilePage = mobilePrimaryPages.some((item) => item.id === route.page)
    ? route.page
    : "more";
  const reportSnapshot = useCallback((snapshot: SnapshotSummary) => {
    setSnapshotWarning((current) => snapshotWarningAfterWrite(current, snapshot));
  }, []);

  return (
    <div className={sidebarCollapsed ? "app-shell sidebar-collapsed" : "app-shell"}>
      <a className="skip-link" href="#main-content">跳到主要内容</a>
      <aside className="sidebar">
        <div className="sidebar-brand">
          <span className="brand-mark">行</span>
          <div><b>山河行记</b></div>
          <button
            className="sidebar-toggle"
            type="button"
            aria-label={sidebarCollapsed ? "展开左侧菜单" : "收起左侧菜单"}
            aria-expanded={!sidebarCollapsed}
            title={sidebarCollapsed ? "展开左侧菜单" : "收起左侧菜单"}
            onClick={() => setSidebarCollapsed((current) => !current)}
          >
            <SidebarSimple aria-hidden="true" size={18} />
          </button>
        </div>
        <div className="trip-mini">
          <span>当前旅行</span>
          <strong>
            {activeTrip?.title ?? (trips.isLoading ? "读取中…" : "尚未创建旅行")}
          </strong>
          <small>
            {activeTrip
              ? formatDateRange(activeTrip.startDate, activeTrip.endDate)
              : trips.isLoading ? "正在读取…" : "请从所有旅行开始"}
          </small>
        </div>
        <div className="sidebar-user">
          <div className="avatar" aria-hidden="true">
            {session.user.displayName.slice(0, 1).toUpperCase()}
          </div>
          <div>
            <b>{session.user.displayName}</b>
          </div>
          <button
            type="button"
            aria-label="退出登录"
            onClick={async () => {
              if (await confirmLeave()) onLogout();
            }}
          >
            <SignOut aria-hidden="true" size={16} /><span>退出</span>
          </button>
        </div>
        <Link
          className={route.page === "trips" ? "all-trips-link is-active" : "all-trips-link"}
          to={pagePath("", "trips")}
          aria-label="所有旅行"
        >
          <AirplaneTilt aria-hidden="true" size={18} />
          <span>所有旅行</span>
        </Link>
        <Link
          className={route.page === "library" ? "all-trips-link is-active" : "all-trips-link"}
          to={pagePath("", "library")}
          aria-label="基础资料"
        >
          <Database aria-hidden="true" size={18} />
          <span>基础资料</span>
        </Link>
        {activeTrip && (
          <nav aria-label="旅行页面" className="nav-group primary-nav">
            {desktopPrimaryPages.map((item) => (
              <Link
                to={linkFor(item.id)}
                key={item.id}
                className={route.page === item.id ? "nav-link nav-active" : "nav-link"}
                aria-current={route.page === item.id ? "page" : undefined}
                aria-label={item.label}
              >
                <item.icon
                  aria-hidden="true"
                  size={18}
                  weight={route.page === item.id ? "fill" : "regular"}
                />
                <span>{item.label}</span>
              </Link>
            ))}
          </nav>
        )}
      </aside>
      <main className="main" id="main-content" tabIndex={-1}>
        <header className="topbar">
          <div className="topbar-title">
            <strong>{route.page === "today" ? "执行日" : pageLabel(route.page)}</strong>
            {route.page !== "trips" && (
              <span>{activeTrip?.title ?? "正在读取旅行…"}</span>
            )}
          </div>
          <div className="topbar-actions">
            {route.page !== "trips" && route.page !== "library" && (
              <Link
                className="topbar-trips-link"
                to={pagePath("", "trips")}
                aria-label="打开所有旅行"
              >
                <AirplaneTilt aria-hidden="true" size={17} />
                <span>所有旅行</span>
              </Link>
            )}
            {route.page !== "trips" && route.page !== "library" && <label className="trip-switcher">
              <span>旅行</span>
              <select
                aria-label="切换旅行"
                value={activeTrip?.id ?? ""}
                onChange={(event) => navigateTo(pagePath(event.target.value, "today"))}
              >
                {trips.data?.trips.map((trip) => (
                  <option key={trip.id} value={trip.id}>{trip.title}</option>
                ))}
              </select>
            </label>}
            {route.page === "trips" && (
              <Link className="topbar-trips-link" to={pagePath("", "library")}>
                <Database aria-hidden="true" size={17} />
                <span>基础资料</span>
              </Link>
            )}
            {route.page === "library" && (
              <Link className="topbar-trips-link" to={pagePath("", "trips")}>
                <AirplaneTilt aria-hidden="true" size={17} />
                <span>所有旅行</span>
              </Link>
            )}
            <span className="environment-status"><i aria-hidden="true" />个人服务器</span>
            <div className="topbar-personal-actions">
              <ThemeToggle />
              <button
                className="mobile-logout-button"
                type="button"
                aria-label="退出登录"
                title="退出登录"
                onClick={async () => {
                  if (await confirmLeave()) onLogout();
                }}
              >
                <SignOut aria-hidden="true" size={18} />
              </button>
            </div>
          </div>
        </header>
        {(error || snapshotWarning) && (
          <div className="toast-stack">
            {error && (
              <div className="toast-error" role="alert" aria-live="assertive">
                <div><b>操作未完成</b><p>{error}</p></div>
                <button type="button" onClick={() => setError("")} aria-label="关闭错误提示">
                  关闭
                </button>
              </div>
            )}
            {snapshotWarning && (
              <div className="toast-warning" role="status" aria-live="polite">
                <div><b>备份状态</b><p>{snapshotWarning}</p></div>
                <button
                  type="button"
                  onClick={() => setSnapshotWarning("")}
                  aria-label="关闭备份提示"
                >
                  关闭
                </button>
              </div>
            )}
          </div>
        )}
        {trips.isError ? (
          <DataError message="旅行列表读取失败" onRetry={() => trips.refetch()} />
        ) : route.page === "library" ? (
          <LibraryPage
            admin={session.user.admin}
            currentMemberId={session.user.id}
            mobile={session.user.mobile}
            setError={setError}
            onSnapshot={reportSnapshot}
            onDirtyChange={setHasUnsavedChanges}
            onUserChange={(user) => onSessionChange({ ...session, user })}
          />
        ) : missingRequestedTrip ? (
          <section className="empty-panel">
            <AirplaneTilt aria-hidden="true" size={32} weight="duotone" />
            <h1>旅行不存在或当前账号无权访问</h1>
            <p>这个链接对应的旅行不可用。系统没有替你切换到其他旅行，以免误操作。</p>
            <button
              className="primary-button"
              type="button"
              onClick={() => navigateTo(pagePath("", "trips"))}
            >
              返回所有旅行
            </button>
          </section>
        ) : route.page === "trips" || (!activeTrip && trips.isSuccess) ? (
          <TripsDashboard
            trips={trips.data?.trips ?? []}
            admin={session.user.admin}
            onOpenTrip={(trip) => guardedNavigate(pagePath(trip.id, "today"))}
            onCreatedTrip={(trip) => {
              navigateAfterSuccessfulWrite(pagePath(trip.id, "itinerary"));
            }}
            setError={setError}
            onSnapshot={reportSnapshot}
            onDirtyChange={setHasUnsavedChanges}
          />
        ) : activeTrip ? (
          <ActivePage
            route={route}
            trip={activeTrip}
            currentMemberId={session.user.id}
            setError={setError}
            onSnapshot={reportSnapshot}
            onDirtyChange={setHasUnsavedChanges}
            navigateTo={guardedNavigate}
          />
        ) : trips.isLoading ? <PageSkeleton /> : <EmptyTrips />}
      </main>
      {activeTrip && route.page !== "trips" && route.page !== "library" && (
        <>
          <div
            className={mobileMoreOpen ? "mobile-more-layer is-open" : "mobile-more-layer"}
            aria-hidden={!mobileMoreOpen}
          >
            <button
              className="mobile-more-dismiss"
              type="button"
              aria-label="关闭更多菜单"
              tabIndex={-1}
              onClick={() => {
                setMobileMoreContext("");
                mobileMoreButtonRef.current?.focus();
              }}
            />
            <nav
              ref={mobileMoreMenuRef}
              id="mobile-more-menu"
              className="mobile-more-menu"
              aria-label="更多页面"
            >
              {mobileMorePages.map((item) => (
                <Link
                  key={item.id}
                  className="mobile-more-item"
                  to={linkFor(item.id)}
                  tabIndex={mobileMoreOpen ? 0 : -1}
                  onClick={() => {
                    setMobileMoreContext("");
                    mobileMoreButtonRef.current?.focus();
                  }}
                >
                  <span>{item.label}</span>
                  <i><item.icon aria-hidden="true" size={20} weight="duotone" /></i>
                </Link>
              ))}
            </nav>
          </div>
          <nav className="mobile-bottom-nav" aria-label="主要页面">
            {mobilePrimaryPages.map((item) => {
              const active = mobilePage === item.id;
              if (item.id === "more") {
                return (
                  <button
                    ref={mobileMoreButtonRef}
                    key={item.id}
                    type="button"
                    className={active || mobileMoreOpen ? "is-active" : ""}
                    aria-expanded={mobileMoreOpen}
                    aria-controls="mobile-more-menu"
                    onClick={() => setMobileMoreContext((current) => (
                      current === mobileMoreContextKey ? "" : mobileMoreContextKey
                    ))}
                  >
                    <item.icon
                      aria-hidden="true"
                      size={21}
                      weight={active || mobileMoreOpen ? "fill" : "regular"}
                    />
                    <span>{item.label}</span>
                  </button>
                );
              }
              return (
                <Link
                  key={item.id}
                  to={linkFor(item.id)}
                  className={active ? "is-active" : ""}
                  aria-current={active ? "page" : undefined}
                >
                  <item.icon aria-hidden="true" size={21} weight={active ? "fill" : "regular"} />
                  <span>{item.label}</span>
                </Link>
              );
            })}
          </nav>
        </>
      )}
    </div>
  );
}

function ActivePage({
  route,
  trip,
  currentMemberId,
  setError,
  onSnapshot,
  onDirtyChange,
  navigateTo
}: {
  route: AppRoute;
  trip: Trip;
  currentMemberId: string;
  setError: (message: string) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
  navigateTo: (path: string, replace?: boolean) => void;
}) {
  if (route.page === "today") {
    return (
      <DayConsole
        trip={trip}
        currentMemberId={currentMemberId}
        date={route.date}
        onDateChange={(date, replace) => navigateTo(pagePath(trip.id, "today", date), replace)}
        onOpenItinerary={() => navigateTo(pagePath(trip.id, "itinerary"))}
        setError={setError}
        onSnapshot={onSnapshot}
        onDirtyChange={onDirtyChange}
      />
    );
  }
  if (route.page === "itinerary") {
    return (
      <ItineraryPage
        trip={trip}
        onOpenDate={(date) => navigateTo(pagePath(trip.id, "today", date))}
        setError={setError}
        onSnapshot={onSnapshot}
        onDirtyChange={onDirtyChange}
      />
    );
  }
  if (route.page === "map") {
    return (
      <MapPage
        trip={trip}
        date={route.date}
        onDateChange={(date, replace) => (
          navigateTo(`${pagePath(trip.id, "map")}?date=${date}`, replace)
        )}
      />
    );
  }
  if (route.page === "more") {
    return <MorePage trip={trip} date={route.date} />;
  }
  if (route.page === "audit") {
    return <AuditLogPage trip={trip} />;
  }
  return (
    <ResourceView
      key={`${trip.id}:${route.page}`}
      trip={trip}
      resource={route.page as PageResource}
      currentMemberId={currentMemberId}
      setError={setError}
      onSnapshot={onSnapshot}
      onDirtyChange={onDirtyChange}
    />
  );
}
