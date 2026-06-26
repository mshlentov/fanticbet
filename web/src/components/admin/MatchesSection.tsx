import { useMemo, useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  cancelMatch,
  createMatch,
  listAdminLeagues,
  setMatchLive,
  setMatchScores,
} from "../../api/admin";
import type { MatchMarketInput } from "../../api/admin";
import { listEvents } from "../../api/events";
import type { Event, EventStatus } from "../../api/types";
import { useToast } from "../../hooks/useToast";
import { EmptyState, ErrorState, LoadingState } from "../states";
import { errMessage } from "../../lib/apiError";
import { marketTitle, sportLabel } from "../../lib/labels";
import { absTime, fmtOdds } from "../../lib/format";

// Ключ списка матчей админки — отдельный от ленты, чтобы инвалидировать после
// создания/расчёта/отмены без влияния на публичные queries.
const MATCHES_KEY = "admin-matches";

// Виды спорта, где есть ничья: для них в ML добавляется исход draw.
const SPORTS_WITH_DRAW = new Set(["football", "hockey"]);

// MatchesSection — создание спортивных матчей (manual) и управление ими: ввод
// финального счёта (авторасчёт ML+TOTALS), перевод в live, отмена.
export function MatchesSection() {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <CreateMatchForm />
      <MatchesList />
    </div>
  );
}

// --- Создание матча ---

function CreateMatchForm() {
  const qc = useQueryClient();
  const { toast } = useToast();

  const leaguesQuery = useQuery({
    queryKey: ["admin-leagues"],
    queryFn: () => listAdminLeagues(),
  });
  const leagues = leaguesQuery.data?.items ?? [];

  const [leagueId, setLeagueId] = useState("");
  const [home, setHome] = useState("");
  const [away, setAway] = useState("");
  const [startsAt, setStartsAt] = useState("");

  // ML-коэффициенты (draw — только для футбола/хоккея).
  const [oddsHome, setOddsHome] = useState("");
  const [oddsDraw, setOddsDraw] = useState("");
  const [oddsAway, setOddsAway] = useState("");

  // Опциональный рынок TOTALS.
  const [totalsOn, setTotalsOn] = useState(false);
  const [line, setLine] = useState("");
  const [oddsOver, setOddsOver] = useState("");
  const [oddsUnder, setOddsUnder] = useState("");

  const selectedLeague = leagues.find((l) => String(l.id) === leagueId);
  const withDraw = selectedLeague ? SPORTS_WITH_DRAW.has(selectedLeague.sport_slug) : false;

  const mutation = useMutation({
    mutationFn: createMatch,
    onSuccess: () => {
      toast("Матч создан", "ok");
      setHome("");
      setAway("");
      setStartsAt("");
      setOddsHome("");
      setOddsDraw("");
      setOddsAway("");
      setTotalsOn(false);
      setLine("");
      setOddsOver("");
      setOddsUnder("");
      qc.invalidateQueries({ queryKey: [MATCHES_KEY] });
    },
    onError: (err) => toast(errMessage(err, "Не удалось создать матч"), "err"),
  });

  // oddsValid — коэффициент > 1.0.
  const oddsValid = (v: string) => Number(v) > 1;

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();

    if (!selectedLeague) {
      toast("Выберите чемпионат", "err");
      return;
    }
    const h = home.trim();
    const a = away.trim();
    if (!h || !a) {
      toast("Укажите обе команды", "err");
      return;
    }

    // ML обязателен. Собираем исходы: home / [draw] / away.
    const mlOutcomes = [
      { code: "home", label: h, odds: oddsHome.trim() },
      ...(withDraw ? [{ code: "draw", label: "Ничья", odds: oddsDraw.trim() }] : []),
      { code: "away", label: a, odds: oddsAway.trim() },
    ];
    if (mlOutcomes.some((o) => !oddsValid(o.odds))) {
      toast("Коэффициенты ML должны быть больше 1.0", "err");
      return;
    }

    const markets: MatchMarketInput[] = [{ type: "ML", outcomes: mlOutcomes }];

    if (totalsOn) {
      const ln = line.trim();
      if (!ln || Number.isNaN(Number(ln))) {
        toast("Укажите линию тотала", "err");
        return;
      }
      if (!oddsValid(oddsOver) || !oddsValid(oddsUnder)) {
        toast("Коэффициенты тотала должны быть больше 1.0", "err");
        return;
      }
      markets.push({
        type: "TOTALS",
        line: ln,
        outcomes: [
          { code: "over", label: `Больше ${ln}`, odds: oddsOver.trim() },
          { code: "under", label: `Меньше ${ln}`, odds: oddsUnder.trim() },
        ],
      });
    }

    mutation.mutate({
      title: `${h} — ${a}`,
      league_id: selectedLeague.id,
      // datetime-local даёт локальное время без зоны — приводим к RFC3339 (UTC).
      starts_at: new Date(startsAt).toISOString(),
      home: h,
      away: a,
      markets,
    });
  };

  return (
    <div className="fb-card fb-anim" style={{ padding: 22 }}>
      <div style={{ fontSize: 15, fontWeight: 800, marginBottom: 14 }}>Новый матч</div>

      {leaguesQuery.isSuccess && leagues.length === 0 && (
        <EmptyState>Сначала создайте чемпионат во вкладке «Чемпионаты».</EmptyState>
      )}

      <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <label className="fb-field">
          Чемпионат
          <select
            className="fb-input"
            required
            value={leagueId}
            onChange={(e) => setLeagueId(e.target.value)}
          >
            <option value="" disabled>
              Выберите чемпионат
            </option>
            {leagues.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name} · {sportLabel(l.sport_slug)}
              </option>
            ))}
          </select>
        </label>

        <div style={{ display: "flex", gap: 8 }}>
          <label className="fb-field" style={{ flex: 1 }}>
            Хозяева
            <input
              className="fb-input"
              type="text"
              placeholder="Манчестер Юнайтед"
              required
              value={home}
              onChange={(e) => setHome(e.target.value)}
            />
          </label>
          <label className="fb-field" style={{ flex: 1 }}>
            Гости
            <input
              className="fb-input"
              type="text"
              placeholder="Ливерпуль"
              required
              value={away}
              onChange={(e) => setAway(e.target.value)}
            />
          </label>
        </div>

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

        <div className="fb-field" style={{ gap: 8 }}>
          Исход матча (ML)
          <div style={{ display: "flex", gap: 8 }}>
            <OddsInput label="П1" value={oddsHome} onChange={setOddsHome} />
            {withDraw && <OddsInput label="X" value={oddsDraw} onChange={setOddsDraw} />}
            <OddsInput label="П2" value={oddsAway} onChange={setOddsAway} />
          </div>
        </div>

        <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, fontWeight: 600 }}>
          <input
            type="checkbox"
            checked={totalsOn}
            onChange={(e) => setTotalsOn(e.target.checked)}
          />
          Добавить тотал (TOTALS)
        </label>

        {totalsOn && (
          <div className="fb-field" style={{ gap: 8 }}>
            <div style={{ display: "flex", gap: 8 }}>
              <label className="fb-field" style={{ width: 100 }}>
                Линия
                <input
                  className="fb-input"
                  type="number"
                  step="0.5"
                  placeholder="2.5"
                  value={line}
                  onChange={(e) => setLine(e.target.value)}
                />
              </label>
              <OddsInput label="Больше" value={oddsOver} onChange={setOddsOver} />
              <OddsInput label="Меньше" value={oddsUnder} onChange={setOddsUnder} />
            </div>
          </div>
        )}

        <button
          type="submit"
          className="fb-btn"
          disabled={mutation.isPending || leagues.length === 0}
        >
          {mutation.isPending ? "Создаём…" : "Создать матч"}
        </button>
      </form>
    </div>
  );
}

// OddsInput — компактное поле ввода коэффициента с подписью.
function OddsInput({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="fb-field" style={{ flex: 1 }}>
      <span style={{ fontSize: 12 }}>{label}</span>
      <input
        className="fb-input"
        type="number"
        step="0.01"
        min="1.01"
        placeholder="Кэф"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}

// --- Список матчей ---

const STATUS_FILTERS: { value: EventStatus; label: string }[] = [
  { value: "upcoming", label: "Предстоящие" },
  { value: "live", label: "Live" },
  { value: "settled", label: "Рассчитанные" },
  { value: "cancelled", label: "Отменённые" },
];

function MatchesList() {
  const [status, setStatus] = useState<EventStatus>("upcoming");

  const query = useQuery({
    queryKey: [MATCHES_KEY, status],
    queryFn: () => listEvents({ status, source: "manual" }),
  });

  const matches = useMemo(() => query.data?.items ?? [], [query.data]);

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
        <div style={{ fontSize: 15, fontWeight: 800 }}>Матчи</div>
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
      {query.isSuccess && matches.length === 0 && (
        <EmptyState>В этой категории пока нет матчей.</EmptyState>
      )}

      {query.isSuccess && matches.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {matches.map((m) => (
            <MatchAdminCard key={m.id} match={m} invalidateKey={MATCHES_KEY} />
          ))}
        </div>
      )}
    </div>
  );
}

function MatchAdminCard({
  match,
  invalidateKey,
}: {
  match: Event;
  invalidateKey: string;
}) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [home, setHome] = useState("");
  const [away, setAway] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: [invalidateKey] });

  const scores = useMutation({
    mutationFn: (vars: { home: number; away: number }) =>
      setMatchScores(match.id, vars.home, vars.away),
    onSuccess: () => {
      toast("Матч рассчитан", "ok");
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось рассчитать матч"), "err"),
  });

  const live = useMutation({
    mutationFn: () => setMatchLive(match.id),
    onSuccess: () => {
      toast("Матч переведён в live", "ok");
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось перевести в live"), "err"),
  });

  const cancel = useMutation({
    mutationFn: () => cancelMatch(match.id),
    onSuccess: () => {
      toast("Матч отменён, ставки возвращены", "ok");
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось отменить матч"), "err"),
  });

  const busy = scores.isPending || live.isPending || cancel.isPending;
  const canScore = match.status === "upcoming" || match.status === "live";

  const handleSettle = () => {
    const h = Number(home);
    const a = Number(away);
    if (!Number.isInteger(h) || !Number.isInteger(a) || h < 0 || a < 0) {
      toast("Счёт — целые неотрицательные числа", "err");
      return;
    }
    if (!window.confirm(`Рассчитать «${match.title}» по счёту ${h}:${a}? Ставки будут рассчитаны.`)) {
      return;
    }
    scores.mutate({ home: h, away: a });
  };

  const handleCancel = () => {
    if (!window.confirm(`Отменить «${match.title}»? Все ставки будут возвращены.`)) return;
    cancel.mutate();
  };

  return (
    <div className="fb-card fb-anim" style={{ padding: 18 }}>
      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 12 }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 14.5, fontWeight: 700 }}>{match.title}</div>
          <div style={{ fontSize: 12, color: "var(--text3)", marginTop: 2 }}>
            #{match.id} · {absTime(match.starts_at)}
            {match.league_name ? ` · ${match.league_name}` : ""}
          </div>
        </div>
        <StatusBadge status={match.status} />
      </div>

      {/* Рынки с текущими коэффициентами (read-only) */}
      <div style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 12 }}>
        {match.markets.map((m) => (
          <div key={m.id}>
            <div
              style={{
                fontSize: 10.5,
                fontWeight: 700,
                color: "var(--text3)",
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                marginBottom: 5,
              }}
            >
              {marketTitle(m)}
            </div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
              {m.outcomes.map((o) => {
                const won = o.result === "won";
                return (
                  <span
                    key={o.id}
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: 6,
                      padding: "6px 10px",
                      borderRadius: 9,
                      border: "1px solid",
                      borderColor: won ? "var(--accent)" : "var(--border)",
                      background: won ? "var(--accent-soft)" : "var(--surface2)",
                      fontSize: 12.5,
                      fontWeight: 600,
                    }}
                  >
                    {o.label}
                    <b style={{ opacity: 0.85 }}>{fmtOdds(o.odds)}</b>
                    {won && <span style={{ color: "var(--green)", fontWeight: 800 }}>✓</span>}
                  </span>
                );
              })}
            </div>
          </div>
        ))}
      </div>

      {/* Управление матчем */}
      {canScore && (
        <div style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 14 }}>
          <div style={{ display: "flex", gap: 8, alignItems: "flex-end", flexWrap: "wrap" }}>
            <label className="fb-field" style={{ width: 110 }}>
              Счёт хозяев
              <input
                className="fb-input"
                type="number"
                min="0"
                placeholder="0"
                value={home}
                onChange={(e) => setHome(e.target.value)}
              />
            </label>
            <label className="fb-field" style={{ width: 110 }}>
              Счёт гостей
              <input
                className="fb-input"
                type="number"
                min="0"
                placeholder="0"
                value={away}
                onChange={(e) => setAway(e.target.value)}
              />
            </label>
            <button
              type="button"
              className="fb-btn"
              disabled={busy || home === "" || away === ""}
              onClick={handleSettle}
              style={{ flex: 1, minWidth: 140 }}
            >
              {scores.isPending ? "Рассчитываем…" : "Рассчитать"}
            </button>
          </div>

          <div style={{ display: "flex", gap: 8 }}>
            {match.status === "upcoming" && (
              <button
                type="button"
                className="fb-btn-outline"
                disabled={busy}
                onClick={() => live.mutate()}
              >
                {live.isPending ? "…" : "В LIVE"}
              </button>
            )}
            <button
              type="button"
              className="fb-btn-outline"
              disabled={busy}
              onClick={handleCancel}
            >
              {cancel.isPending ? "Отменяем…" : "Отменить"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: EventStatus }) {
  const map: Record<EventStatus, { cls: string; label: string }> = {
    upcoming: { cls: "fb-badge-upcoming", label: "Предстоит" },
    live: { cls: "fb-badge-live", label: "LIVE" },
    settled: { cls: "fb-badge-settled", label: "Рассчитан" },
    cancelled: { cls: "fb-badge-settled", label: "Отменён" },
  };
  const { cls, label } = map[status];
  return <span className={`fb-badge ${cls}`} style={{ flexShrink: 0 }}>{label}</span>;
}
