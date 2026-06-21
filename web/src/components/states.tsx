import type { ReactNode } from "react";

// Общие состояния загрузки/ошибки/пустого списка в едином стиле макета.

export function LoadingState({ label = "Загрузка…" }: { label?: string }) {
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "center",
        padding: "64px 0",
        color: "var(--text3)",
        fontSize: 14,
      }}
    >
      <span style={{ animation: "livePulse 1.4s ease infinite" }}>{label}</span>
    </div>
  );
}

export function ErrorState({
  message = "Не удалось загрузить данные",
  onRetry,
}: {
  message?: string;
  onRetry?: () => void;
}) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        gap: 12,
        padding: "64px 0",
        textAlign: "center",
      }}
    >
      <p style={{ color: "var(--red)", margin: 0, fontSize: 14 }}>{message}</p>
      {onRetry && (
        <button type="button" className="fb-btn-outline" onClick={onRetry}>
          Повторить
        </button>
      )}
    </div>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return (
    <div
      style={{
        textAlign: "center",
        padding: "40px 20px",
        color: "var(--text3)",
        fontSize: 14,
        background: "var(--surface)",
        border: "1px dashed var(--border)",
        borderRadius: 16,
      }}
    >
      {children}
    </div>
  );
}
