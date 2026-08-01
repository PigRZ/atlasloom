// 页面内确认框统一承载危险操作与未保存修改提示，不依赖浏览器原生弹窗。
import { WarningCircle } from "@phosphor-icons/react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode
} from "react";
import { createPortal } from "react-dom";

type ConfirmOptions = {
  title: string;
  message: string;
  confirmLabel?: string;
  tone?: "default" | "danger";
  acknowledgeOnly?: boolean;
};

type PendingConfirm = ConfirmOptions & {
  resolve: (confirmed: boolean) => void;
};

const ConfirmContext = createContext<
  ((options: ConfirmOptions) => Promise<boolean>) | null
>(null);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const titleId = useId();
  const messageId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const confirm = useCallback((options: ConfirmOptions) => new Promise<boolean>((resolve) => {
    setPending((current) => {
      current?.resolve(false);
      return { ...options, resolve };
    });
  }), []);
  const settle = useCallback((confirmed: boolean) => {
    setPending((current) => {
      current?.resolve(confirmed);
      return null;
    });
  }, []);

  useEffect(() => {
    if (!pending) return undefined;
    const opener = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    const appRoot = document.getElementById("root");
    const previousInert = appRoot?.inert ?? false;
    const previousOverflow = document.body.style.overflow;
    if (appRoot) appRoot.inert = true;
    document.body.style.overflow = "hidden";
    const frame = window.requestAnimationFrame(() => (
      pending.acknowledgeOnly ? confirmRef.current?.focus() : cancelRef.current?.focus()
    ));
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        settle(false);
        return;
      }
      if (event.key === "Tab") {
        const controls = [...(dialogRef.current?.querySelectorAll<HTMLElement>(
          "button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex='-1'])"
        ) ?? [])];
        const first = controls[0];
        const last = controls.at(-1);
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
    document.addEventListener("keydown", onKeyDown);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
      if (appRoot) appRoot.inert = previousInert;
      window.requestAnimationFrame(() => {
        if (opener?.isConnected) opener.focus();
      });
    };
  }, [pending, settle]);

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {pending && createPortal(
        <div className="confirm-backdrop">
          <section
            ref={dialogRef}
            className="confirm-dialog"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby={titleId}
            aria-describedby={messageId}
          >
            <WarningCircle
              className={pending.tone === "danger" ? "is-danger" : ""}
              aria-hidden="true"
              size={28}
              weight="duotone"
            />
            <h2 id={titleId}>{pending.title}</h2>
            <p id={messageId}>{pending.message}</p>
            <div className={pending.acknowledgeOnly ? "is-single" : undefined}>
              {!pending.acknowledgeOnly && (
                <button
                  ref={cancelRef}
                  className="secondary-button"
                  type="button"
                  onClick={() => settle(false)}
                >
                  取消
                </button>
              )}
              <button
                ref={confirmRef}
                className={pending.tone === "danger"
                  ? "secondary-button danger-button"
                  : "primary-button"}
                type="button"
                onClick={() => settle(true)}
              >
                {pending.confirmLabel ?? "确定"}
              </button>
            </div>
          </section>
        </div>,
        document.body
      )}
    </ConfirmContext.Provider>
  );
}

export function useConfirm() {
  const confirm = useContext(ConfirmContext);
  if (!confirm) throw new Error("useConfirm 必须在 ConfirmProvider 内使用");
  return confirm;
}
