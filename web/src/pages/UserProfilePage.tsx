import { Link, useParams } from "react-router-dom";

// UserProfilePage — публичный профиль (статистика + история ставок). Эндпоинты
// /users/:id относятся к вехе M5 — пока on-brand заглушка.
export function UserProfilePage() {
  const { id } = useParams<{ id: string }>();
  return (
    <section style={{ maxWidth: 720, margin: "0 auto" }}>
      <Link to="/leaderboard" style={{ fontSize: 13, fontWeight: 600, color: "var(--text2)" }}>
        ← Лидерборд
      </Link>
      <div className="fb-card fb-anim" style={{ padding: 28, marginTop: 14, textAlign: "center" }}>
        <div style={{ fontSize: 15, fontWeight: 700, marginBottom: 6 }}>Профиль #{id}</div>
        <p style={{ margin: 0, fontSize: 13.5, color: "var(--text3)" }}>
          Публичная статистика и история ставок пользователя появятся в M5.
        </p>
      </div>
    </section>
  );
}
