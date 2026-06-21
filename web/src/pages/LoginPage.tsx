import { useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";

import { useAuth } from "../hooks/useAuth";
import { ApiError } from "../api/client";
import { oauthLoginUrl } from "../api/auth";

type LocationState = { from?: string } | null;

// LoginPage — вход по email+паролю + OAuth (Яндекс/VK). После успеха возвращает
// на исходный путь (если приходили из ProtectedRoute) или на /.
export function LoginPage() {
  const { status, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as LocationState)?.from ?? "/";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  if (status === "authenticated") return <Navigate to="/" replace />;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(email, password);
      navigate(from, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Не удалось войти, попробуйте снова");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ maxWidth: 380, margin: "24px auto" }}>
      <div className="fb-card fb-anim" style={{ padding: 26 }}>
        <div style={{ fontSize: 20, fontWeight: 800, letterSpacing: "-0.02em", marginBottom: 4 }}>
          Вход в FanticBet
        </div>
        <div style={{ fontSize: 13, color: "var(--text3)", marginBottom: 18 }}>
          С возвращением! Введите данные аккаунта.
        </div>

        <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <input
            className="fb-input"
            type="email"
            placeholder="Email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <input
            className="fb-input"
            type="password"
            placeholder="Пароль"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {error && <p style={{ margin: 0, fontSize: 13, color: "var(--red)" }}>{error}</p>}
          <button type="submit" className="fb-btn" disabled={submitting}>
            {submitting ? "Входим…" : "Войти"}
          </button>
        </form>

        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            color: "var(--text3)",
            fontSize: 12,
            fontWeight: 600,
            margin: "16px 0",
          }}
        >
          <span style={{ flex: 1, height: 1, background: "var(--border)" }} />
          или
          <span style={{ flex: 1, height: 1, background: "var(--border)" }} />
        </div>

        <div style={{ display: "flex", gap: 8 }}>
          <a href={oauthLoginUrl("yandex")} className="fb-btn-outline" style={{ flex: 1, textAlign: "center" }}>
            Яндекс
          </a>
          <a href={oauthLoginUrl("vk")} className="fb-btn-outline" style={{ flex: 1, textAlign: "center" }}>
            VK
          </a>
        </div>

        <p style={{ marginTop: 18, textAlign: "center", fontSize: 13, color: "var(--text2)" }}>
          Нет аккаунта?{" "}
          <Link to="/register" style={{ fontWeight: 700, color: "var(--accent)" }}>
            Зарегистрироваться
          </Link>
        </p>
      </div>
    </div>
  );
}
