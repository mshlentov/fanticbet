import { useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";

import { useAuth } from "../hooks/useAuth";
import { ApiError } from "../api/client";

// RegisterPage — регистрация. Бэкенд создаёт user+wallet и начисляет
// signup-бонус; по успеху пользователь сразу авторизован.
export function RegisterPage() {
  const { status, register } = useAuth();
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  if (status === "authenticated") return <Navigate to="/" replace />;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await register(email, password, displayName);
      navigate("/", { replace: true });
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Не удалось зарегистрироваться, попробуйте снова",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={{ maxWidth: 380, margin: "24px auto" }}>
      <div className="fb-card fb-anim" style={{ padding: 26 }}>
        <div style={{ fontSize: 20, fontWeight: 800, letterSpacing: "-0.02em", marginBottom: 4 }}>
          Регистрация
        </div>
        <div style={{ fontSize: 13, color: "var(--text3)", marginBottom: 18 }}>
          Создайте аккаунт и получите стартовый бонус фантиков.
        </div>

        <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <input
            className="fb-input"
            type="text"
            placeholder="Имя"
            required
            minLength={2}
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
          />
          <input
            className="fb-input"
            type="email"
            placeholder="Email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <div>
            <input
              className="fb-input"
              type="password"
              placeholder="Пароль"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <span style={{ fontSize: 11, color: "var(--text3)" }}>Минимум 8 символов</span>
          </div>
          {error && <p style={{ margin: 0, fontSize: 13, color: "var(--red)" }}>{error}</p>}
          <button type="submit" className="fb-btn" disabled={submitting}>
            {submitting ? "Создаём аккаунт…" : "Зарегистрироваться"}
          </button>
        </form>

        <p style={{ marginTop: 18, textAlign: "center", fontSize: 13, color: "var(--text2)" }}>
          Уже есть аккаунт?{" "}
          <Link to="/login" style={{ fontWeight: 700, color: "var(--accent)" }}>
            Войти
          </Link>
        </p>
      </div>
    </div>
  );
}
