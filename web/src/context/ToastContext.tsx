import { createContext, useCallback, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";

export type ToastKind = "ok" | "err" | "info";

type Toast = {
  id: number;
  text: string;
  color: string;
};

export type ToastContextValue = {
  // toast — показать уведомление; само исчезнет через ~3.4 с.
  toast: (text: string, kind?: ToastKind) => void;
};

const COLORS: Record<ToastKind, string> = {
  ok: "var(--green)",
  err: "var(--red)",
  info: "var(--accent)",
};

// eslint-disable-next-line react-refresh/only-export-components
export const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(0);

  const toast = useCallback((text: string, kind: ToastKind = "info") => {
    const id = ++nextId.current;
    setToasts((prev) => [...prev, { id, text, color: COLORS[kind] }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 3400);
  }, []);

  const value = useMemo<ToastContextValue>(() => ({ toast }), [toast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="fb-toasts">
        {toasts.map((t) => (
          <div key={t.id} className="fb-toast">
            <span className="fb-toast-dot" style={{ background: t.color }} />
            <span className="fb-toast-text">{t.text}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
