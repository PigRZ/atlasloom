// 顶层应用只装配路由与错误边界，不持有 Trip 业务状态。
import { createBrowserRouter, RouterProvider } from "react-router";
import { TravelApp } from "./app/TravelApp";
import { PageErrorBoundary } from "./components/PageStates";

const router = createBrowserRouter(
  [{ path: "*", element: <TravelApp /> }],
  { basename: import.meta.env.BASE_URL }
);

export function App() {
  return (
    <PageErrorBoundary>
      <RouterProvider router={router} />
    </PageErrorBoundary>
  );
}
