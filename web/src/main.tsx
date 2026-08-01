// 浏览器入口只创建 React 根节点并装配 QueryClient 与应用组件。
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import { ConfirmProvider } from "./components/ConfirmDialog";
import { initializeTheme } from "./lib/theme";
import "./styles/app.css";

initializeTheme();

const client = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true
    }
  }
});
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <ConfirmProvider>
        <App />
      </ConfirmProvider>
    </QueryClientProvider>
  </StrictMode>
);
