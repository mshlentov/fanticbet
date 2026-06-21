import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useBetslip } from "../hooks/useBetslip";
import { useAuth } from "../hooks/useAuth";
import { useToast } from "../hooks/useToast";
import { placeBet } from "../api/bets";
import { ApiError } from "../api/client";
import { fmtCoins, fmtOdds } from "../lib/format";
import { CoinIcon } from "./icons";

// Пределы ставки в макете. Истинные пределы валидирует бэкенд (BET_MIN/BET_MAX),
// здесь — только подсказка и ранняя проверка для UX.
const STAKE_MIN = 10;
const STAKE_MAX = 10000;

// Betslip — купон ставок (сайдбар / нижний лист на мобильном). Отправляет
// каждую позицию отдельным POST /bets, затем обновляет баланс и историю.
export function Betslip() {
  const { items, open, setOpen, remove, setStake, clear } = useBetslip();
  const { status, balance, refresh } = useAuth();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const placeMutation = useMutation({
    mutationFn: async () => {
      // Последовательно: на исходе может закрыться рынок — важен порядок и
      // понятная ошибка по первой неудачной ставке.
      for (const it of items) {
        await placeBet(it.outcomeId, parseInt(it.stake, 10));
      }
    },
    onSuccess: async () => {
      const total = items.reduce((a, i) => a + parseInt(i.stake, 10), 0);
      clear();
      setOpen(false);
      await refresh(); // подтянуть актуальный баланс в шапку
      await queryClient.invalidateQueries({ queryKey: ["my-bets"] });
      toast(`Ставка принята! Списано ${fmtCoins(total)} фантиков`, "ok");
    },
    onError: (err) => {
      const msg =
        err instanceof ApiError ? err.message : "Не удалось разместить ставку";
      toast(msg, "err");
      // Баланс/история могли частично измениться — синхронизируем.
      void refresh();
      void queryClient.invalidateQueries({ queryKey: ["my-bets"] });
    },
  });

  const handlePlace = () => {
    if (status !== "authenticated") {
      toast("Войдите, чтобы делать ставки", "err");
      navigate("/login");
      return;
    }
    let total = 0;
    for (const it of items) {
      const v = parseInt(it.stake, 10);
      if (!v || v < STAKE_MIN || v > STAKE_MAX) {
        toast(`Сумма каждой ставки — от ${STAKE_MIN} до ${fmtCoins(STAKE_MAX)} фантиков`, "err");
        return;
      }
      total += v;
    }
    if (balance !== null && total > balance) {
      toast("Недостаточно фантиков на балансе", "err");
      return;
    }
    placeMutation.mutate();
  };

  if (!open) return null;

  const hasItems = items.length > 0;
  let totalN = 0;
  let potentialN = 0;
  for (const it of items) {
    const v = parseInt(it.stake, 10);
    if (v > 0) {
      totalN += v;
      potentialN += Math.floor(v * Number(it.odds));
    }
  }

  return (
    <div className="fb-betslip" role="dialog" aria-label="Купон ставок">
      {/* Шапка купона */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 9,
          padding: "14px 16px",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <span style={{ fontSize: 15, fontWeight: 800 }}>Купон ставок</span>
        <span
          style={{
            background: "var(--accent-soft)",
            color: "var(--accent)",
            fontSize: 12,
            fontWeight: 800,
            padding: "2px 9px",
            borderRadius: 999,
          }}
        >
          {items.length}
        </span>
        <div style={{ flex: 1 }} />
        {hasItems && (
          <button
            type="button"
            onClick={clear}
            style={{
              background: "none",
              border: "none",
              padding: 0,
              cursor: "pointer",
              fontSize: 12.5,
              fontWeight: 600,
              color: "var(--text3)",
            }}
          >
            Очистить
          </button>
        )}
        <button
          type="button"
          onClick={() => setOpen(false)}
          aria-label="Закрыть"
          style={{
            width: 28,
            height: 28,
            borderRadius: 8,
            border: "none",
            background: "var(--surface2)",
            color: "var(--text2)",
            cursor: "pointer",
            fontSize: 15,
            lineHeight: 1,
            display: "grid",
            placeItems: "center",
          }}
        >
          ×
        </button>
      </div>

      {/* Тело: позиции или пустое состояние */}
      <div
        style={{
          flex: 1,
          overflow: "auto",
          padding: 14,
          display: "flex",
          flexDirection: "column",
          gap: 10,
        }}
      >
        {!hasItems && (
          <div
            style={{
              flex: 1,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              gap: 10,
              textAlign: "center",
              padding: 20,
            }}
          >
            <span style={{ opacity: 0.3 }}>
              <CoinIcon size={44} />
            </span>
            <div style={{ fontSize: 14, fontWeight: 700, color: "var(--text2)" }}>
              Купон пуст
            </div>
            <div style={{ fontSize: 12.5, color: "var(--text3)", maxWidth: 220 }}>
              Нажмите на коэффициент в любом событии, чтобы добавить ставку
            </div>
          </div>
        )}

        {items.map((it) => {
          const v = parseInt(it.stake, 10);
          const potential = v > 0 ? fmtCoins(Math.floor(v * Number(it.odds))) : "—";
          return (
            <div
              key={it.outcomeId}
              style={{
                border: "1px solid var(--border)",
                borderRadius: 13,
                padding: "11px 12px",
                display: "flex",
                flexDirection: "column",
                gap: 9,
                background: "var(--bg)",
                animation: "fadeUp .2s ease both",
              }}
            >
              <div style={{ display: "flex", alignItems: "flex-start", gap: 8 }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: 13.5, fontWeight: 700 }}>
                    {it.outcomeLabel}
                  </div>
                  <div
                    style={{
                      fontSize: 11.5,
                      color: "var(--text3)",
                      marginTop: 2,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {it.eventTitle} · {it.marketLabel}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => remove(it.outcomeId)}
                  aria-label="Удалить"
                  style={{
                    width: 24,
                    height: 24,
                    borderRadius: 7,
                    border: "none",
                    background: "var(--surface2)",
                    color: "var(--text3)",
                    cursor: "pointer",
                    fontSize: 14,
                    lineHeight: 1,
                    display: "grid",
                    placeItems: "center",
                    flexShrink: 0,
                  }}
                >
                  ×
                </button>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <span
                  style={{
                    padding: "5px 10px",
                    borderRadius: 8,
                    background: "var(--accent-soft)",
                    color: "var(--accent)",
                    fontWeight: 800,
                    fontSize: 13.5,
                    flexShrink: 0,
                  }}
                >
                  {fmtOdds(it.odds)}
                </span>
                <input
                  type="number"
                  className="fb-input"
                  value={it.stake}
                  min={STAKE_MIN}
                  max={STAKE_MAX}
                  onChange={(e) => setStake(it.outcomeId, e.target.value)}
                  style={{
                    flex: 1,
                    minWidth: 0,
                    padding: "8px 11px",
                    borderRadius: 9,
                    background: "var(--surface)",
                    fontSize: 14,
                    fontWeight: 700,
                  }}
                />
                <span style={{ fontSize: 12, color: "var(--text2)", whiteSpace: "nowrap" }}>
                  → <span style={{ fontWeight: 800, color: "var(--text)" }}>{potential}</span>
                </span>
              </div>
            </div>
          );
        })}
      </div>

      {/* Подвал: суммы и кнопка */}
      {hasItems && (
        <div
          style={{
            borderTop: "1px solid var(--border)",
            padding: "14px 16px",
            display: "flex",
            flexDirection: "column",
            gap: 8,
          }}
        >
          <div style={{ display: "flex", justifyContent: "space-between", fontSize: 13, color: "var(--text2)", fontWeight: 600 }}>
            <span>Сумма ставок</span>
            <span style={{ color: "var(--text)", fontWeight: 800 }}>{fmtCoins(totalN)}</span>
          </div>
          <div style={{ display: "flex", justifyContent: "space-between", fontSize: 13, color: "var(--text2)", fontWeight: 600 }}>
            <span>Потенциальный выигрыш</span>
            <span style={{ color: "var(--green)", fontWeight: 800 }}>{fmtCoins(potentialN)}</span>
          </div>
          <button
            type="button"
            className="fb-btn"
            style={{ marginTop: 5, fontSize: 15 }}
            disabled={placeMutation.isPending}
            onClick={handlePlace}
          >
            {placeMutation.isPending ? "Отправляем…" : "Сделать ставку"}
          </button>
          <div style={{ fontSize: 11, color: "var(--text3)", textAlign: "center" }}>
            Мин. {STAKE_MIN} · Макс. {fmtCoins(STAKE_MAX)} фантиков на исход
          </div>
        </div>
      )}
    </div>
  );
}

// SlipFab — плавающая кнопка открытия купона (когда купон свёрнут, но не пуст).
export function SlipFab() {
  const { items, open, setOpen } = useBetslip();
  if (open || items.length === 0) return null;
  return (
    <div className="fb-slip-fab">
      <button type="button" onClick={() => setOpen(true)} aria-label="Открыть купон">
        <svg
          width="23"
          height="23"
          viewBox="0 0 24 24"
          fill="none"
          stroke="#ffffff"
          strokeWidth="1.8"
          strokeLinejoin="round"
        >
          <path d="M3 9V7a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v2a2.5 2.5 0 0 0 0 6v2a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-2a2.5 2.5 0 0 0 0-6z" />
          <path d="M14 5v14" strokeDasharray="2 2.6" />
        </svg>
        <span className="count">{items.length}</span>
      </button>
    </div>
  );
}
