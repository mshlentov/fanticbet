import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  createLeague,
  deleteLeague,
  listAdminLeagues,
  updateLeague,
} from "../../api/admin";
import type { League } from "../../api/types";
import { useToast } from "../../hooks/useToast";
import { EmptyState, ErrorState, LoadingState } from "../states";
import { errMessage } from "../../lib/apiError";
import { sportLabel } from "../../lib/labels";

// Ключ списка чемпионатов — общий для админки, чтобы фильтр спорта матчей и
// форма матча видели свежий справочник после мутаций.
export const LEAGUES_KEY = "admin-leagues";

// Виды спорта для селекта (совпадают с sportLabel; 'custom' исключён — лиги к
// кастомным событиям не привязываются).
const SPORTS: { value: string; label: string }[] = [
  { value: "football", label: "Футбол" },
  { value: "basketball", label: "Баскетбол" },
  { value: "hockey", label: "Хоккей" },
  { value: "tennis", label: "Теннис" },
];

// LeaguesSection — CRUD чемпионатов: форма создания + список с inline-правкой и
// удалением. Удаление лиги с привязанными событиями вернёт 409 (показываем тост).
export function LeaguesSection() {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <CreateLeagueForm />
      <LeaguesList />
    </div>
  );
}

function CreateLeagueForm() {
  const qc = useQueryClient();
  const { toast } = useToast();

  const [name, setName] = useState("");
  const [sportSlug, setSportSlug] = useState(SPORTS[0].value);

  const mutation = useMutation({
    mutationFn: (vars: { name: string; sportSlug: string }) =>
      createLeague(vars.name, vars.sportSlug),
    onSuccess: () => {
      toast("Чемпионат создан", "ok");
      setName("");
      qc.invalidateQueries({ queryKey: [LEAGUES_KEY] });
    },
    onError: (err) => toast(errMessage(err, "Не удалось создать чемпионат"), "err"),
  });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      toast("Укажите название чемпионата", "err");
      return;
    }
    mutation.mutate({ name: name.trim(), sportSlug });
  };

  return (
    <div className="fb-card fb-anim" style={{ padding: 22 }}>
      <div style={{ fontSize: 15, fontWeight: 800, marginBottom: 14 }}>Новый чемпионат</div>

      <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", gap: 8 }}>
          <label className="fb-field" style={{ flex: 1 }}>
            Название
            <input
              className="fb-input"
              type="text"
              placeholder="АПЛ"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <label className="fb-field" style={{ width: 160 }}>
            Вид спорта
            <select
              className="fb-input"
              value={sportSlug}
              onChange={(e) => setSportSlug(e.target.value)}
            >
              {SPORTS.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        <button type="submit" className="fb-btn" disabled={mutation.isPending}>
          {mutation.isPending ? "Создаём…" : "Создать чемпионат"}
        </button>
      </form>
    </div>
  );
}

function LeaguesList() {
  const query = useQuery({
    queryKey: [LEAGUES_KEY],
    queryFn: () => listAdminLeagues(),
  });

  const leagues = query.data?.items ?? [];

  return (
    <div>
      <div style={{ fontSize: 15, fontWeight: 800, marginBottom: 14 }}>Чемпионаты</div>

      {query.isPending && <LoadingState />}
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      {query.isSuccess && leagues.length === 0 && (
        <EmptyState>Чемпионаты ещё не созданы.</EmptyState>
      )}

      {query.isSuccess && leagues.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {leagues.map((l) => (
            <LeagueRow key={l.id} league={l} />
          ))}
        </div>
      )}
    </div>
  );
}

function LeagueRow({ league }: { league: League }) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(league.name);
  const [sportSlug, setSportSlug] = useState(league.sport_slug);

  const invalidate = () => qc.invalidateQueries({ queryKey: [LEAGUES_KEY] });

  const save = useMutation({
    mutationFn: () => updateLeague(league.id, { name: name.trim(), sport_slug: sportSlug }),
    onSuccess: () => {
      toast("Чемпионат обновлён", "ok");
      setEditing(false);
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось обновить чемпионат"), "err"),
  });

  const remove = useMutation({
    mutationFn: () => deleteLeague(league.id),
    onSuccess: () => {
      toast("Чемпионат удалён", "ok");
      invalidate();
    },
    onError: (err) => toast(errMessage(err, "Не удалось удалить чемпионат"), "err"),
  });

  const busy = save.isPending || remove.isPending;

  const handleDelete = () => {
    if (!window.confirm(`Удалить чемпионат «${league.name}»?`)) return;
    remove.mutate();
  };

  if (editing) {
    return (
      <div className="fb-card fb-anim" style={{ padding: 14, display: "flex", gap: 8, alignItems: "center" }}>
        <input
          className="fb-input"
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          style={{ flex: 1 }}
        />
        <select
          className="fb-input"
          value={sportSlug}
          onChange={(e) => setSportSlug(e.target.value)}
          style={{ width: 150 }}
        >
          {SPORTS.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="fb-btn"
          disabled={busy || !name.trim()}
          onClick={() => save.mutate()}
        >
          {save.isPending ? "…" : "Сохранить"}
        </button>
        <button
          type="button"
          className="fb-btn-outline"
          disabled={busy}
          onClick={() => {
            setName(league.name);
            setSportSlug(league.sport_slug);
            setEditing(false);
          }}
        >
          Отмена
        </button>
      </div>
    );
  }

  return (
    <div className="fb-card fb-anim" style={{ padding: 14, display: "flex", gap: 12, alignItems: "center" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 14, fontWeight: 700 }}>{league.name}</div>
        <div style={{ fontSize: 12, color: "var(--text3)", marginTop: 2 }}>
          #{league.id} · {sportLabel(league.sport_slug)}
        </div>
      </div>
      <button
        type="button"
        className="fb-btn-outline"
        disabled={busy}
        onClick={() => setEditing(true)}
      >
        Изменить
      </button>
      <button
        type="button"
        className="fb-btn-outline"
        disabled={busy}
        onClick={handleDelete}
      >
        {remove.isPending ? "Удаляем…" : "Удалить"}
      </button>
    </div>
  );
}
