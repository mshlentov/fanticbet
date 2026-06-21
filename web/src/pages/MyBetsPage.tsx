import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { listMyBets } from "../api/bets";
import { listTransactions } from "../api/user";
import type { Bet, TransactionType, WalletTransaction } from "../api/types";
import { useAuth } from "../hooks/useAuth";
import { CoinIcon } from "../components/icons";
import { EmptyState, ErrorState, LoadingState } from "../components/states";
import { absTime, fmtCoins, fmtOdds } from "../lib/format";
import { BET_STATUS_META, betResultLine } from "../lib/bet";

type Tab = "active" | "done" | "tx";

// Подписи типов транзакций для журнала кошелька.
const TX_LABELS: Record<TransactionType, string> = {
  signup_bonus: "Бонус за регистрацию",
  bet_stake: "Ставка",
  bet_payout: "Выигрыш",
  bet_refund: "Возврат",
  admin_adjust: "Корректировка баланса",
};

// MyBetsPage — кошелёк и история: баланс, статистика (винрейт/прибыль/ROI),
// вкладки активных/завершённых ставок и журнал транзакций.
export function MyBetsPage() {
  const { balance } = useAuth();
  const [tab, setTab] = useState<Tab>("active");

  const betsQuery = useQuery({ queryKey: ["my-bets"], queryFn: () => listMyBets() });
  const txQuery = useQuery({
    queryKey: ["transactions", 1],
    queryFn: () => listTransactions(1),
    enabled: tab === "tx",
  });

  const bets = betsQuery.data?.items ?? [];
  const stats = useMemo(() => computeStats(bets), [bets]);
  const active = bets.filter((b) => b.status === "pending");
  const done = bets.filter((b) => b.status !== "pending");

  const tabs: { key: Tab; label: string }[] = [
    { key: "active", label: `Активные (${active.length})` },
    { key: "done", label: `Завершённые (${done.length})` },
    { key: "tx", label: "Транзакции" },
  ];

  return (
    <section style={{ maxWidth: 860, margin: "0 auto" }}>
      {/* Баланс + статистика */}
      <div
        className="fb-card fb-anim"
        style={{
          padding: 24,
          borderRadius: 18,
          marginBottom: 18,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: 24,
        }}
      >
        <div style={{ flex: 1, minWidth: 200 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 700,
              color: "var(--text3)",
              textTransform: "uppercase",
              letterSpacing: "0.07em",
              marginBottom: 6,
            }}
          >
            Баланс
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 11 }}>
            <CoinIcon size={34} />
            <span style={{ fontSize: 34, fontWeight: 800, letterSpacing: "-0.03em" }}>
              {balance !== null ? fmtCoins(balance) : "—"}
            </span>
            <span style={{ fontSize: 14, color: "var(--text3)", fontWeight: 600, alignSelf: "flex-end", paddingBottom: 5 }}>
              фантиков
            </span>
          </div>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3,minmax(86px,1fr))", gap: 10 }}>
          <div className="fb-stat">
            <div className="fb-stat-label">Винрейт</div>
            <div className="fb-stat-value">{stats.winRate}</div>
          </div>
          <div className="fb-stat">
            <div className="fb-stat-label">Прибыль</div>
            <div className="fb-stat-value" style={{ color: stats.profitColor }}>{stats.profit}</div>
          </div>
          <div className="fb-stat">
            <div className="fb-stat-label">ROI</div>
            <div className="fb-stat-value" style={{ color: stats.profitColor }}>{stats.roi}</div>
          </div>
        </div>
      </div>

      {/* Вкладки */}
      <div className="fb-segment" style={{ marginBottom: 16 }}>
        {tabs.map((t) => (
          <button
            key={t.key}
            type="button"
            className={`fb-segment-btn${tab === t.key ? " is-active" : ""}`}
            onClick={() => setTab(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {betsQuery.isPending && <LoadingState />}
      {betsQuery.isError && <ErrorState onRetry={() => betsQuery.refetch()} />}

      {/* Ставки */}
      {betsQuery.isSuccess && tab !== "tx" && (
        <div style={{ display: "flex", flexDirection: "column", gap: 9 }}>
          {(tab === "active" ? active : done).length === 0 ? (
            <EmptyState>Здесь пока пусто</EmptyState>
          ) : (
            (tab === "active" ? active : done).map((b) => <BetRow key={b.id} bet={b} />)
          )}
        </div>
      )}

      {/* Транзакции */}
      {tab === "tx" && (
        <>
          {txQuery.isPending && <LoadingState />}
          {txQuery.isError && <ErrorState onRetry={() => txQuery.refetch()} />}
          {txQuery.isSuccess && txQuery.data.items.length === 0 && (
            <EmptyState>Транзакций пока нет</EmptyState>
          )}
          {txQuery.isSuccess && txQuery.data.items.length > 0 && (
            <div className="fb-card" style={{ overflow: "hidden" }}>
              {txQuery.data.items.map((t) => (
                <TxRow key={t.id} tx={t} />
              ))}
            </div>
          )}
        </>
      )}
    </section>
  );
}

function BetRow({ bet }: { bet: Bet }) {
  const meta = BET_STATUS_META[bet.status];
  const res = betResultLine(bet);
  return (
    <div
      className="fb-card"
      style={{ display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap", padding: "13px 16px" }}
    >
      <span
        style={{
          fontSize: 11,
          fontWeight: 700,
          padding: "5px 10px",
          borderRadius: 999,
          background: meta.bg,
          color: meta.color,
          flexShrink: 0,
        }}
      >
        {meta.label}
      </span>
      <div style={{ flex: 1, minWidth: 170 }}>
        <div style={{ fontSize: 14, fontWeight: 700, letterSpacing: "-0.01em" }}>
          Событие #{bet.event_id}
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text2)", marginTop: 2 }}>
          Исход #{bet.outcome_id} <span style={{ color: "var(--text3)" }}>· кф {fmtOdds(bet.odds)}</span>
        </div>
      </div>
      <div style={{ textAlign: "right" }}>
        <div style={{ fontSize: 13.5, fontWeight: 800, color: res.color }}>{res.text}</div>
        <div style={{ fontSize: 11.5, color: "var(--text3)", marginTop: 2 }}>
          Ставка {fmtCoins(bet.stake)} · {absTime(bet.created_at)}
        </div>
      </div>
    </div>
  );
}

function TxRow({ tx }: { tx: WalletTransaction }) {
  const positive = tx.amount >= 0;
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 14,
        padding: "13px 18px",
        borderBottom: "1px solid var(--border)",
      }}
    >
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 13.5, fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {TX_LABELS[tx.type] ?? tx.type}
        </div>
        <div style={{ fontSize: 11.5, color: "var(--text3)", marginTop: 2 }}>{absTime(tx.created_at)}</div>
      </div>
      <div style={{ textAlign: "right" }}>
        <div style={{ fontSize: 14, fontWeight: 800, color: positive ? "var(--green)" : "var(--text)" }}>
          {positive ? "+" : "−"}
          {fmtCoins(Math.abs(tx.amount))}
        </div>
        <div style={{ fontSize: 11.5, color: "var(--text3)", marginTop: 2 }}>
          Баланс: {fmtCoins(tx.balance_after)}
        </div>
      </div>
    </div>
  );
}

// computeStats — винрейт/прибыль/ROI по рассчитанным ставкам.
function computeStats(bets: Bet[]) {
  const settled = bets.filter((b) => b.status === "won" || b.status === "lost");
  const won = settled.filter((b) => b.status === "won");
  const profit = settled.reduce(
    (acc, b) => acc + (b.status === "won" ? b.potential_payout - b.stake : -b.stake),
    0,
  );
  const staked = settled.reduce((acc, b) => acc + b.stake, 0);
  const sign = profit >= 0 ? "+" : "−";
  return {
    winRate: settled.length ? `${Math.round((won.length / settled.length) * 100)}%` : "—",
    profit: `${sign}${fmtCoins(Math.abs(profit))}`,
    roi: staked ? `${sign}${Math.abs((profit / staked) * 100).toFixed(1)}%` : "—",
    profitColor: profit >= 0 ? "var(--green)" : "var(--red)",
  };
}
