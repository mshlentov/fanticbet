import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { listEvents } from "../api/events";
import {
  adjustBalance,
  cancelEvent,
  createCustomEvent,
  settleEvent,
} from "../api/admin";
import type { AdminOutcomeInput } from "../api/admin";
import { ApiError } from "../api/client";
import type { Event, EventStatus } from "../api/types";
import { useToast } from "../hooks/useToast";
import { EmptyState, ErrorState, LoadingState } from "../components/states";
import { absTime, fmtCoins, fmtOdds } from "../lib/format";

// Ключ списка кастомных событий — вынесен, чтобы инвалидировать после мутаций.
const EVENTS_KEY = "admin-events";

const STATUS_FILTERS: { value: EventStatus; label: string }[] = [
  { value: "upcoming", label: "Активные" },
  { value: "settled", label: "Рассчитанные" },
  { value: "cancelled", label: "Отменённые" },
];

// errMessage — текст ошибки API для тоста (единый формат бэкенда).
function errMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
}

// AdminPage — управление кастомными событиями (создание, расчёт, отмена) и
// ручная корректировка баланса. Список кастомных событий берём из публичной
// ленты с фильтром sport='custom' (так бэкенд помечает кастомные события).
export function AdminPage() {
  return (
    <section style={{ maxWidth: 760, margin: "0 auto" }}>
      <h1 style={{ margin: "0 0 4px", fontSize: 21, fontWeight: 800, letterSpacing: "-0.02em" }}>
        Админ-панель
      </h1>
      <p style={{ margin: "0 0 18px", fontSize: 13, color: "var(--text3)" }}>
        Кастомные события и корректировки баланса
      </p>

      <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
        <CreateEventForm />
        <AdjustBalanceForm />
        <CustomEventsList />
      </div>
    </section>
  );
}

// --- Создание кастомного события ---

type OutcomeDraft = { label: string; odds: string };

function CreateEventForm() {
  const qc = useQueryClient();
  const { toast } = useToast();

  const [title, setTitle] = useState("");
  const [startsAt, setStartsAt] = useState("");
  const [question, setQuestion] = useState("");
  const [outcomes, setOutcomes] = useState<OutcomeDraft[]>([
    { label: "", odds: "" },
    { label: "", odds: "" },
  ]);

  const mutation = useMutation({
    mutationFn: (vars: {
      title: string;
      starts_at: string;
      question: string;
      outcomes: AdminOutcomeInput[];
    }) =>
      createCustomEvent({
        title: vars.title,
        starts_at: vars.starts_at,
        market: {
          question: vars.question || null,
          outcomes: vars.outcomes,
        },
      }),
    onSuccess: () => {
      toast("Событие создано", "ok");
      setTitle("");
      setStartsAt("");
      setQuestion("");
      setOutcomes([
        { label: "", odds: "" },
        { label: "", odds: "" },
      ]);
      qc.invalidateQueries({ queryKey: [EVENTS_KEY] });
    },
    onError: (err) => toast(errMessage(err, "Не удалось создать событие"), "err"),
  });

  const updateOutcome = (i: number, patch: Partial<OutcomeDraft>) => {
    setOutcomes((prev) => prev.map((o, idx) => (idx === i ? { ...o, ...patch } : o)));
  };

  const addOutcome = () => setOutcomes((prev) => [...prev, { label: "", odds: "" }]);
  const removeOutcome = (i: number) =>
    setOutcomes((prev) => prev.filter((_, idx) => idx !== i));

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();

    const cleaned = outcomes
      .map((o) => ({ label: o.label.trim(), odds: o.odds.trim() }))
      .filter((o) => o.label || o.odds);

    if (cleaned.length < 2) {
      toast("Нужно минимум два исхода", "err");
      return;
    }
    if (cleaned.some((o) => !o.label || !o.odds)) {
      toast("У каждого исхода должны быть название и коэффициент", "err");
      return;
    }
    if (cleaned.some((o) => !(Number(o.odds) > 1))) {
      toast("Коэффициент должен быть больше 1.0", "err");
      return;
    }

    mutation.mutate({
      title: title.trim(),
      // datetime-local даёт локальное время без зоны — приводим к RFC3339 (UTC).
      starts_at: new Date(startsAt).toISOString(),
      question: question.trim(),
      outcomes: cleaned,
    });
  };

  return (
    <div className="fb-card fb-anim" style={{ padding: 22 }}>
      <div style={{ fontSize: 15, fontWeight: 800, marginBottom: 14 }}>
        Новое кастомное событие
      </div>

      <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <label className="fb-field">
          Название
          <input
            className="fb-input"
            type="text"
            placeholder="Кто победит в дебатах?"
            required
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </label>

        <label className="fb-field">
          Старт
          <input
            className="fb-input"
            type="datetime-local"
            required
            value={startsAt}
            onChange={(e) => setStartsAt(e.target.value)}
          />
        </label>

        <label className="fb-field">
          Вопрос рынка (необязательно)
          <input
            className="fb-input"
            type="text"
            placeholder="Победитель"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
          />
        </label>

        <div className="fb-field" style={{ gap: 8 }}>
          Исходы
          {outcomes.map((o, i) => (
            <div key={i} style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <input
                className="fb-input"
                type="text"
                placeholder={`Исход ${i + 1}`}
                value={o.label}
                onChange={(e) => updateOutcome(i, { label: e.target.value })}
                style={{ flex: 1 }}
              />
              <input
                className="fb-input"
                type="number"
                step="0.01"
                min="1.01"
                placeholder="Кэф"
                value={o.odds}
                onChange={(e) => updateOutcome(i, { odds: e.target.value })}
                style={{ width: 92 }}
              />
              <button
                type="button"
                className="fb-icon-btn"
                aria-label="Удалить исход"
                disabled={outcomes.length <= 2}
                onClick={() => removeOutcome(i)}
                style={{ flexShrink: 0 }}
              >
                ×
              </button>
            </div>
          ))}
          <button
            type="button"
            className="fb-btn-outline"
            onClick={addOutcome}
            style={{ alignSelf: "flex-start" }}
          >
            + Добавить исход
          </button>
        </div>

        <button type="submit" className="fb-btn" disabled={mutation.isPending}>
          {mutation.isPending ? "Создаём…" : "Создать событие"}
        </button>
      </form>
    </div>
  );
}

// --- Корректировка баланса ---

function AdjustBalanceForm() {
  const { toast } = useToast();

  const [userId, setUserId] = useState("");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");

  const mutation = useMutation({
    mutationFn: (vars: { userId: number; amount: number; reason: string }) =>
      adjustBalance(vars.userId, vars.amount, vars.reason),
    onSuccess: (res) => {
      toast(`Баланс обновлён: ${fmtCoins(res.balance)} ₣`, "ok");
      setAmount("");
      setReason("");
    },
    onError: (err) => toast(errMessage(err, "Не удалось скорректировать баланс"), "err"),
  });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();

    const id = Number(userId);
    const amt = Number(amount);
    if (!Number.isInteger(id) || id <= 0) {
      toast("Неверный ID пользователя", "err");
      return;
    }
    if (!Number.isInteger(amt) || amt === 0) {
      toast("Сумма должна быть ненулевым целым числом", "err");
      return;
    }
    if (!reason.trim()) {
      toast("Укажите причину", "err");
      return;
    }

    mutation.mutate({ userId: id, amount: amt, reason: reason.trim() });
  };

  return (
    <div className="fb-card fb-anim" style={{ padding: 22 }}>
      <div style={{ fontSize: 15, fontWeight: 800, marginBottom: 4 }}>
        Корректировка баланса
      </div>
      <p style={{ margin: "0 0 14px", fontSize: 12.5, color: "var(--text3)" }}>
        Отрицательная сумма — списание. Причина попадёт в журнал сервера.
      </p>

      <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", gap: 8 }}>
          <label className="fb-field" style={{ width: 120 }}>
            ID игрока
            <input
              className="fb-input"
              type="number"
              min="1"
              placeholder="42"
              required
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
            />
          </label>
          <label className="fb-field" style={{ flex: 1 }}>
            Сумма (₣)
            <input
              className="fb-input"
              type="number"
              step="1"
              placeholder="например, 500 или -200"
              required
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
          </label>
        </div>

        <label className="fb-field">
          Причина
          <input
            className="fb-input"
            type="text"
            placeholder="Компенсация за сбой"
            required
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>

        <button type="submit" className="fb-btn" disabled={mutation.isPending}>
          {mutation.isPending ? "Применяем…" : "Скорректировать"}
        </button>
      </form>
    </div>
  );
}

// --- Список кастомных событий ---

function CustomEventsList() {
  const [status, setStatus] = useState<EventStatus>("upcoming");

  const query = useQuery({
    queryKey: [EVENTS_KEY, status],
    queryFn: () => listEvents({ sport: "custom", status }),
  });

  const events = query.data?.items ?? [];

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          flexWrap: "wrap",
          gap: 10,
          marginBottom: 14,
        }}
      >
        <div style={{ fontSize: 15, fontWeight: 800 }}>Кастомные события</div>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.value}
              type="button"
              className={`fb-chip${status === f.value ? " is-active" : ""}`}
              onClick={() => setStatus(f.value)}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {query.isPending && <LoadingState />}
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      {query.isSuccess && events.length === 0 && (
        <EmptyState>В этой категории пока нет кастомных событий.</EmptyState>
      )}

      {query.isSuccess && events.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {events.map((event) => (
            <EventAdminCard key={event.id} event={event} />
          ))}
        </div>
      )}
    </div>
  );
}

function EventAdminCard({ event }: { event: Event }) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [winner, setWinner] = useState<number | null>(null);

  const market = event.markets[0];
  const outcomes = market?.outcomes ?? [];

  const invalidate = () => qc.invalidateQueries({ queryKey: [EVENTS_KEY] });

  const settle = useMutation({
    mutationFn: (outcomeId: number) => settleEvent(event.id, outcomeId),
    onSuccess: () => {
      toast("Событие рассчитано", "ok");
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось рассчитать событие"), "err"),
  });

  const cancel = useMutation({
    mutationFn: () => cancelEvent(event.id),
    onSuccess: () => {
      toast("Событие отменено, ставки возвращены", "ok");
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось отменить событие"), "err"),
  });

  const busy = settle.isPending || cancel.isPending;
  const isUpcoming = event.status === "upcoming";

  const handleSettle = () => {
    if (winner === null) return;
    const oc = outcomes.find((o) => o.id === winner);
    if (!window.confirm(`Рассчитать «${event.title}»? Победитель: ${oc?.label ?? winner}.`)) {
      return;
    }
    settle.mutate(winner);
  };

  const handleCancel = () => {
    if (!window.confirm(`Отменить «${event.title}»? Все ставки будут возвращены.`)) {
      return;
    }
    cancel.mutate();
  };

  return (
    <div className="fb-card fb-anim" style={{ padding: 18 }}>
      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 12 }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 14.5, fontWeight: 700 }}>{event.title}</div>
          <div style={{ fontSize: 12, color: "var(--text3)", marginTop: 2 }}>
            #{event.id} · {absTime(event.starts_at)}
            {market?.question ? ` · ${market.question}` : ""}
          </div>
        </div>
        <StatusBadge status={event.status} />
      </div>

      {outcomes.length > 0 && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginTop: 14 }}>
          {outcomes.map((o) => {
            const selectable = isUpcoming && !busy;
            const selected = winner === o.id;
            // Для рассчитанных событий подсвечиваем победивший исход.
            const won = o.result === "won";
            return (
              <button
                key={o.id}
                type="button"
                disabled={!selectable}
                onClick={() => selectable && setWinner(o.id)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  padding: "8px 12px",
                  borderRadius: 10,
                  border: "1px solid",
                  borderColor: selected || won ? "var(--accent)" : "var(--border)",
                  background: selected
                    ? "var(--accent)"
                    : won
                      ? "var(--accent-soft)"
                      : "var(--surface2)",
                  color: selected ? "#fff" : "var(--text)",
                  cursor: selectable ? "pointer" : "default",
                  fontSize: 13,
                  fontWeight: 600,
                }}
              >
                <span>{o.label}</span>
                <span style={{ fontWeight: 800, opacity: 0.85 }}>{fmtOdds(o.odds)}</span>
                {won && <span style={{ fontSize: 11, color: "var(--green)", fontWeight: 800 }}>✓</span>}
              </button>
            );
          })}
        </div>
      )}

      {isUpcoming && (
        <div style={{ display: "flex", gap: 8, marginTop: 14 }}>
          <button
            type="button"
            className="fb-btn"
            disabled={winner === null || busy}
            onClick={handleSettle}
            style={{ flex: 1 }}
          >
            {settle.isPending ? "Рассчитываем…" : "Рассчитать"}
          </button>
          <button
            type="button"
            className="fb-btn-outline"
            disabled={busy}
            onClick={handleCancel}
          >
            {cancel.isPending ? "Отменяем…" : "Отменить"}
          </button>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: EventStatus }) {
  const map: Record<EventStatus, { cls: string; label: string }> = {
    upcoming: { cls: "fb-badge-upcoming", label: "Активно" },
    live: { cls: "fb-badge-live", label: "LIVE" },
    settled: { cls: "fb-badge-settled", label: "Рассчитано" },
    cancelled: { cls: "fb-badge-settled", label: "Отменено" },
  };
  const { cls, label } = map[status];
  return <span className={`fb-badge ${cls}`} style={{ flexShrink: 0 }}>{label}</span>;
}
