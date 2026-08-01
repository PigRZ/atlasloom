// TravelApp 只管理会话和浏览器路由，旅行与日期状态由 URL 唯一表达。
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { LoadingScreen } from "../components/PageStates";
import { LoginPage } from "../features/auth/LoginPage";
import { TravelShell } from "../features/trips/TravelShell";
import { api, setCSRFToken } from "../lib/api";
import type { Session } from "../lib/types";
import { appRouteFromPath } from "./navigation";

type SessionState = {
  value: Session | null;
  generation: number;
  error: string;
};

export function TravelApp() {
  const [sessionState, setSessionState] = useState<SessionState>({
    value: null,
    generation: 0,
    error: ""
  });
  const [checkingSession, setCheckingSession] = useState(true);
  const queryClient = useQueryClient();
  const location = useLocation();
  const navigate = useNavigate();
  const session = sessionState.value;
  const route = useMemo(() => {
    const parsed = appRouteFromPath(location.pathname);
    if (!parsed.date) {
      parsed.date = new URLSearchParams(location.search).get("date") ?? "";
    }
    return parsed;
  }, [location.pathname, location.search]);
  const startSession = useCallback((next: Session) => {
    queryClient.clear();
    setCSRFToken(next.csrfToken);
    setSessionState((current) => ({
      value: next,
      generation: current.generation + 1,
      error: ""
    }));
  }, [queryClient]);
  const setSessionError = useCallback((error: string) => {
    setSessionState((current) => (
      current.generation === sessionState.generation
        ? { ...current, error }
        : current
    ));
  }, [sessionState.generation]);
  const updateCurrentSession = useCallback((next: Session) => {
    setSessionState((current) => (
      current.generation === sessionState.generation
      && current.value?.user.id === next.user.id
      && current.value.csrfToken === next.csrfToken
        ? { ...current, value: next }
        : current
    ));
  }, [sessionState.generation]);

  useEffect(() => {
    document.documentElement.scrollTop = 0;
    document.body.scrollTop = 0;
  }, [route.date, route.page, route.tripId]);

  useEffect(() => {
    let active = true;
    queryClient.clear();
    setCSRFToken("");
    api.session()
      .then((next) => {
        if (!active) return;
        startSession(next);
      })
      .catch(() => undefined)
      .finally(() => {
        if (active) setCheckingSession(false);
      });
    return () => {
      active = false;
    };
  }, [queryClient, startSession]);

  if (checkingSession) return <LoadingScreen />;
  if (!session) {
    return (
      <LoginPage
        onSession={startSession}
      />
    );
  }

  const logout = () => api.logout().finally(() => {
    setCSRFToken("");
    queryClient.clear();
    setSessionState((current) => ({
      value: null,
      generation: current.generation + 1,
      error: ""
    }));
  });
  return (
    <TravelShell
      session={session}
      route={route}
      navigateTo={(path, replace = false) => navigate(path, { replace })}
      error={sessionState.error}
      setError={setSessionError}
      onLogout={logout}
      onSessionChange={updateCurrentSession}
    />
  );
}
