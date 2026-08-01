// 编辑浮层统一管理焦点、滚动锁和脏表单关闭确认。
import { X } from "@phosphor-icons/react";
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  type ReactNode
} from "react";
import { createPortal } from "react-dom";
import { useConfirm } from "./ConfirmDialog";

type EditorSurfaceProps = {
  open: boolean;
  title: string;
  variant: "modal" | "drawer";
  layer?: "default" | "nested";
  dirty: boolean;
  onClose: () => void;
  children: ReactNode;
};

type EditorStackEntry = {
  id: symbol;
  backdrop: HTMLDivElement;
};

const editorStack: EditorStackEntry[] = [];
let bodyOverflowBeforeEditors: string | null = null;

function syncEditorStack() {
  const top = editorStack.at(-1);
  editorStack.forEach((entry) => {
    const inactive = entry !== top;
    entry.backdrop.inert = inactive;
    if (inactive) entry.backdrop.setAttribute("aria-hidden", "true");
    else entry.backdrop.removeAttribute("aria-hidden");
  });
}

function removeEditorFromStack(id: symbol) {
  const index = editorStack.findIndex((entry) => entry.id === id);
  if (index >= 0) editorStack.splice(index, 1);
  if (editorStack.length === 0) {
    document.body.style.overflow = bodyOverflowBeforeEditors ?? "";
    bodyOverflowBeforeEditors = null;
  } else {
    document.body.style.overflow = "hidden";
  }
  syncEditorStack();
}

const focusableSelector = [
  "button:not([disabled])",
  "[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])"
].join(",");

export function EditorSurface({
  open,
  title,
  variant,
  layer = "default",
  dirty,
  onClose,
  children
}: EditorSurfaceProps) {
  const titleId = useId();
  const editorId = useRef(Symbol("editor-surface"));
  const confirm = useConfirm();
  const backdropRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const requestClose = useCallback(async () => {
    if (!dirty || await confirm({
      title: "放弃未保存修改？",
      message: "当前表单有未保存的修改，关闭后不会保留。",
      confirmLabel: "放弃修改",
      tone: "danger"
    })) onClose();
  }, [confirm, dirty, onClose]);
  const requestCloseRef = useRef(requestClose);

  useLayoutEffect(() => {
    requestCloseRef.current = requestClose;
  }, [requestClose]);

  useEffect(() => {
    if (!open || !backdropRef.current) return undefined;
    const currentEditorId = editorId.current;
    const stackEntry = {
      id: currentEditorId,
      backdrop: backdropRef.current
    };
    if (editorStack.length === 0) {
      bodyOverflowBeforeEditors = document.body.style.overflow;
    }
    editorStack.push(stackEntry);
    syncEditorStack();
    const previousFocus = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    document.body.style.overflow = "hidden";
    const frame = window.requestAnimationFrame(() => {
      panelRef.current?.querySelector<HTMLElement>(focusableSelector)?.focus();
    });
    const onKeyDown = (event: KeyboardEvent) => {
      if (editorStack.at(-1)?.id !== currentEditorId) return;
      if (event.key === "Escape") {
        event.preventDefault();
        requestCloseRef.current();
        return;
      }
      if (event.key !== "Tab" || !panelRef.current) return;
      const focusable = [...panelRef.current.querySelectorAll<HTMLElement>(focusableSelector)];
      const first = focusable[0];
      const last = focusable.at(-1);
      if (!first || !last) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", onKeyDown);
      removeEditorFromStack(currentEditorId);
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [open]);

  if (!open) return null;
  return createPortal(
    <div
      className={`editor-backdrop editor-${variant}${layer === "nested" ? " editor-nested" : ""}`}
      ref={backdropRef}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) requestClose();
      }}
    >
      <div
        className="editor-surface"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        ref={panelRef}
      >
        <header className="editor-surface-heading">
          <h2 id={titleId}>{title}</h2>
          <button type="button" onClick={requestClose} aria-label={`关闭${title}`}>
            <X aria-hidden="true" size={18} weight="bold" />
          </button>
        </header>
        <div className="editor-surface-body">{children}</div>
      </div>
    </div>,
    document.body
  );
}
