import { Link } from "react-router-dom";

// NotFoundPage — 404 для неизвестных маршрутов.
export function NotFoundPage() {
  return (
    <div style={{ padding: "64px 0", textAlign: "center" }}>
      <h1 style={{ margin: "0 0 8px", fontSize: 30, fontWeight: 800 }}>404</h1>
      <p style={{ margin: "0 0 16px", color: "var(--text3)" }}>Страница не найдена</p>
      <Link to="/" style={{ fontWeight: 700, color: "var(--accent)" }}>
        На главную
      </Link>
    </div>
  );
}
