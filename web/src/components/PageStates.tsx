// 通用页面状态组件只处理加载、空态和错误隔离，不读取领域数据。
import { WarningCircle } from "@phosphor-icons/react";
import { Component, type ErrorInfo, type ReactNode } from "react";

export class PageErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("page-render-failed", error, info.componentStack);
  }

  render() {
    if (this.state.failed) {
      return (
        <main className="fatal-state">
          <div className="fatal-card">
            <WarningCircle className="state-icon" aria-hidden="true" size={30} weight="duotone" />
            <h1>页面加载失败</h1>
            <p>数据没有被修改。刷新后仍失败，请记录当前页面名称。</p>
            <button className="primary-button" type="button" onClick={() => window.location.reload()}>
              重新加载
            </button>
          </div>
        </main>
      );
    }
    return this.props.children;
  }
}

export function LoadingScreen() {
  return (
    <main className="loading-screen" role="status" aria-live="polite">
      <span className="loading-mark">行</span>
      <span>正在连接旅行数据…</span>
    </main>
  );
}

export function EmptyTrips() {
  return (
    <section className="empty-panel">
      <h1>当前成员没有旅行</h1>
      <p>请联系旅行创建者，为当前成员授予访问权限。</p>
    </section>
  );
}

export function PageSkeleton() {
  return (
    <div className="page-skeleton" role="status" aria-live="polite">
      <span className="sr-only">正在加载…</span>
      <div aria-hidden="true" />
      <div aria-hidden="true" />
      <div aria-hidden="true" />
    </div>
  );
}

export function DataError({ message, onRetry }: { message: string; onRetry: () => unknown }) {
  return (
    <section className="data-error" role="alert">
      <div>
        <b>{message}</b>
        <p>检查网络连接后重试。已经保存的数据不会受影响。</p>
      </div>
      <button type="button" onClick={onRetry}>重试</button>
    </section>
  );
}
