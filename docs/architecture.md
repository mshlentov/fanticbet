# FanticBet — Архитектура MVP

Платформа ставок на виртуальную валюту («фантики») на спортивные события и кастомные события, создаваемые админом. Документ рассчитан на одного разработчика и постепенное развитие: сначала минимальное ядро, потом наращивание.

> **Текущая фаза — M8 «Ручное управление событиями».** Интеграция с Odds-API.io **временно приостановлена**: воркеры синхронизации событий/коэффициентов/settlement не запускаются, пока `ODDS_API_KEY` пуст (см. `cmd/server/main.go`). Вместо этого события, коэффициенты и результаты вводит админ вручную — как спортивные матчи (с командами, чемпионатом, рынками ML/TOTALS и финальным счётом), так и произвольные события. Подробности — раздел 11 и веха M8 в `tasks.md`. Пакет `internal/oddsapi` и воркеры сохранены в коде для повторного включения без переписывания.

---

## 1. Принципиальные решения

| Решение | Выбор | Почему |
|---|---|---|
| Бэкенд | Go + Gin, **монолит** | Один сервис, один деплой. Микросервисы для MVP — лишняя сложность |
| БД | PostgreSQL 16 | Транзакции критичны для кошелька и ставок; JSONB для счетов матчей |
| ORM / доступ к БД | GORM (или sqlc, если хочется ближе к SQL) | GORM быстрее для старта; миграции — через golang-migrate отдельно от GORM |
| Кэш | Без Redis в MVP | In-memory кэш для лидерборда. Коэффициенты живут в Postgres |
| Источник событий | **Ручной ввод админом (M8)**; Odds-API — на паузе | На время M8 коэффициенты и результаты вносит админ через `/admin/*`. Воркеры Odds-API отключены |
| Расчёт ставок | По `scores` для спортивных (ML, TOTALS); по победившему исходу для произвольных | `SettlementService` уже умеет оба варианта — переиспользуется для ручных матчей |
| Авторизация | Email+пароль (bcrypt) + OAuth2 (Google, VK, Яндекс) через библиотеку `goth` | JWT (access 15 мин) + refresh-токен в httpOnly cookie |
| Валюта | Фантики хранятся **целым числом** (int64) | Никаких float для денег. Коэффициенты — NUMERIC(8,3) |
| Фронтенд | React + TypeScript + Vite, SPA поверх REST API | Стандартный стек, много материалов для обучения |
| Деплой | Docker Compose (app + postgres + nginx) на одном VPS | Минимум DevOps |

Сознательно **вне MVP**: live-ставки, экспрессы (парлеи), WebSocket от Odds-API, Redis, нотификации, мобильное приложение. Архитектура их не блокирует — см. раздел 9.

---

## 2. Общая схема

```
                ┌─────────────────────────────────────────────┐
                │                  FanticBet (Go)              │
                │                                              │
 Browser ◄────► │  Gin HTTP API                                │
 (React SPA)    │   ├─ /api/v1/auth/...   (auth handlers)      │
                │   ├─ /api/v1/events,bets,users,leaderboard   │
                │   └─ /api/v1/admin/...  (role=admin)         │
                │                                              │
                │  Service layer (бизнес-логика)               │
                │   ├─ AuthService, UserService                │
                │   ├─ BettingService (ставка, кошелёк)        │
                │   ├─ SettlementService (расчёт)              │
                │   ├─ AdminService (ручные события, лиги)     │
                │   └─ StatsService (профиль, лидерборд)       │
                │                                              │
                │  Repository layer (Postgres)                 │
                │                                              │
                │  Background workers — НА ПАУЗЕ (M8):         │
                │   воркеры синхронизации/расчёта Odds-API     │
                │   не запускаются без ODDS_API_KEY            │
                │   ┌──────────────────────────────────┐       │
                │   │ (код сохранён) EventSync/OddsSync│ ──┐   │
                │   │ (код сохранён) Settlement        │   │ на повторное
                │   └──────────────────────────────────┘   │  включение
                │                                          ▼  │
                │                                    Odds-API.io │
                │                                    (пауза в M8)│
                └──────────────────┬──────────────────────────┘
                                   │
                              PostgreSQL
```

Слои внутри монолита: `handler → service → repository`. Handlers не содержат бизнес-логики, services не знают про HTTP, repository не знает про бизнес-правила. Воркеры используют те же services/repositories.

### Структура проекта

```
fanticbet/
├── cmd/server/main.go            # точка входа: конфиг, DI, запуск HTTP + воркеров
├── internal/
│   ├── config/                   # env-конфиг (порт, DSN, ключи OAuth, ODDS_API_KEY)
│   ├── domain/                   # структуры: User, Event, Market, Outcome, Bet, Tx
│   ├── handler/                  # Gin-хендлеры + middleware (auth, admin, CORS)
│   ├── service/                  # бизнес-логика
│   ├── repository/               # работа с Postgres
│   ├── oddsapi/                  # HTTP-клиент Odds-API.io — НА ПАУЗЕ (M8); сохранён для повторного включения
│   └── worker/                   # event_sync.go, odds_sync.go, settlement.go — НА ПАУЗЕ (M8)
├── migrations/                   # golang-migrate, *.up.sql / *.down.sql
├── web/                          # фронтенд (React, отдельный package.json)
├── docker-compose.yml
└── .env.example
```

---

## 3. Модель данных и схема БД

Ключевая идея: единая модель `events → markets → outcomes` и для спортивных (oddsapi/manual), и для произвольных (custom) событий. Ставка ссылается на `outcome` и **фиксирует коэффициент на момент ставки** — коэффициенты в `outcomes` потом меняются (в M8 — админом через `/admin/*`; позже — снова воркером), но ставку это не затрагивает.

### Чемпионаты/лиги (M8)

Чемпионат — отдельная сущность: чемпионат группирует события (АПЛ, НБА, «Кубок двора» и т.д.) и нужен для фильтров ленты и админки. Заводится админом. На событие ссылается `events.league_id` (внешний ключ); `events.league_name` остаётся денормализованной текстовой копией — удобно для oddsapi-событий (лига приходит из API как строка) и для выборок без join. У custom-событий `league_id` может быть NULL.

```sql
-- ПОЛЬЗОВАТЕЛИ И АВТОРИЗАЦИЯ ------------------------------------------------

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT UNIQUE,                  -- NULL допустим, если вход только через OAuth без email
    password_hash TEXT,                          -- NULL для чисто-OAuth пользователей
    display_name  TEXT NOT NULL,
    avatar_url    TEXT,
    role          TEXT NOT NULL DEFAULT 'user',  -- 'user' | 'admin'
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Привязки внешних провайдеров: у одного user может быть несколько (Google + VK)
CREATE TABLE auth_identities (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id),
    provider         TEXT NOT NULL,              -- 'google' | 'vk' | 'yandex'
    provider_user_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE refresh_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    token_hash TEXT NOT NULL,                    -- хранится sha256, не сам токен
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

-- КОШЕЛЁК --------------------------------------------------------------------

CREATE TABLE wallets (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id),
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Журнал всех движений фантиков (append-only). Баланс кошелька всегда
-- должен сходиться с суммой транзакций — это ваша защита от багов.
CREATE TABLE wallet_transactions (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id),
    amount        BIGINT NOT NULL,               -- + начисление, − списание
    type          TEXT NOT NULL,                 -- 'signup_bonus' | 'bet_stake' | 'bet_payout'
                                                 -- | 'bet_refund' | 'admin_adjust'
    bet_id        BIGINT,                        -- ссылка на ставку, если применимо
    balance_after BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wtx_user ON wallet_transactions(user_id, created_at DESC);

-- СОБЫТИЯ, РЫНКИ, ИСХОДЫ ------------------------------------------------------

-- Чемпионаты/лиги (M8). Группируют события (АПЛ, НБА, ...). Заводятся админом;
-- у oddsapi-событий лига создаётся/подтягивается воркером как строка (при
-- повторном включении). sport_slug — чтобы фильтровать лиги по виду спорта.
CREATE TABLE leagues (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    sport_slug  TEXT NOT NULL,                   -- 'football', 'basketball', ...; 'custom' — не используется
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_leagues_sport ON leagues(sport_slug);

CREATE TABLE events (
    id           BIGSERIAL PRIMARY KEY,
    source       TEXT NOT NULL,                  -- 'oddsapi' | 'manual' | 'custom'
    external_id  BIGINT,                         -- id события в Odds-API (NULL для manual/custom)
    sport_slug   TEXT NOT NULL,                  -- 'football', ... ; 'custom' для произвольных
    league_id    BIGINT REFERENCES leagues(id),  -- ссылка на чемпионат; NULL для произвольных/oddsapi без лиги
    league_name  TEXT,                           -- денормализованное имя (копия leagues.name / строка из API)
    title        TEXT NOT NULL,                  -- "Manchester United — Liverpool" или текст произвольного
    home         TEXT,                           -- NULL для произвольных
    away         TEXT,
    starts_at    TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'upcoming',
                  -- 'upcoming' | 'live' | 'settled' | 'cancelled'
    scores       JSONB,                          -- {"home":N,"away":N} — для расчёта ML/TOTALS; NULL, пока счёта нет
    created_by   BIGINT REFERENCES users(id),    -- админ-автор manual/custom события
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);
CREATE INDEX idx_events_status_start ON events(status, starts_at);
CREATE INDEX idx_events_league ON events(league_id);

CREATE TABLE markets (
    id        BIGSERIAL PRIMARY KEY,
    event_id  BIGINT NOT NULL REFERENCES events(id),
    type      TEXT NOT NULL,                     -- 'ML' | 'TOTALS' | 'CUSTOM'
    line      NUMERIC(6,2),                      -- 2.5 для тоталов; NULL для ML/CUSTOM
    question  TEXT,                              -- текст вопроса для CUSTOM
    status    TEXT NOT NULL DEFAULT 'open'       -- 'open' | 'suspended' | 'settled' | 'void'
);
CREATE INDEX idx_markets_event ON markets(event_id);

CREATE TABLE outcomes (
    id        BIGSERIAL PRIMARY KEY,
    market_id BIGINT NOT NULL REFERENCES markets(id),
    code      TEXT NOT NULL,                     -- 'home'|'draw'|'away'|'over'|'under'|'opt_N'
    label     TEXT NOT NULL,                     -- отображаемое имя
    odds      NUMERIC(8,3) NOT NULL CHECK (odds > 1.0),  -- ТЕКУЩИЙ коэффициент
    result    TEXT                               -- NULL | 'won' | 'lost' | 'void'
);
CREATE INDEX idx_outcomes_market ON outcomes(market_id);

-- СТАВКИ -----------------------------------------------------------------------

CREATE TABLE bets (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id),
    outcome_id       BIGINT NOT NULL REFERENCES outcomes(id),
    event_id         BIGINT NOT NULL REFERENCES events(id),   -- денормализация для выборок
    stake            BIGINT NOT NULL CHECK (stake > 0),
    odds             NUMERIC(8,3) NOT NULL,       -- коэффициент, ЗАФИКСИРОВАННЫЙ при ставке
    potential_payout BIGINT NOT NULL,             -- floor(stake * odds)
    status           TEXT NOT NULL DEFAULT 'pending',
                      -- 'pending' | 'won' | 'lost' | 'void'
    settled_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bets_user    ON bets(user_id, created_at DESC);
CREATE INDEX idx_bets_event   ON bets(event_id) WHERE status = 'pending';
```

Замечания к схеме:

- **История ставок публична по продукту**, поэтому `GET /users/:id/bets` отдаёт ставки любого пользователя — отдельных настроек приватности в MVP нет (можно добавить флаг `users.is_history_public` позже).
- Лидерборд в MVP считается агрегатным SQL-запросом по `bets` (profit = Σ payout − Σ stake по рассчитанным ставкам), без отдельной таблицы. При росте — материализованное представление или таблица `leaderboard_snapshots`, обновляемая воркером.
- Для событий из Odds-API (когда интеграция снова включена) храним **одного букмекера** (задаётся конфигом) и два рынка: ML и основной тотал. Для ручных матчей (M8) те же рынки ML/TOTALS создаёт админ вместе с коэффициентами — бюрократии с букмекером нет.

---

## 4. REST API

Префикс `/api/v1`. Авторизация: `Authorization: Bearer <access JWT>`; refresh — httpOnly cookie. Ответы — JSON, ошибки в формате `{"error": {"code": "...", "message": "..."}}`.

**Интерактивная документация (Swagger/OpenAPI).** API описывается аннотациями `swaggo/swag` прямо над хендлерами (теги `@Summary`, `@Param`, `@Success`, `@Router`, `@Security BearerAuth`). Спека генерируется командой `swag init -g cmd/server/main.go -o docs/swagger` (или `make swagger`) в пакет `docs/swagger` (`swagger.json` + `swagger.yaml` + `docs.go`); сгенерированные файлы коммитятся, чтобы сборка не зависела от наличия CLI. UI поднимается на `GET /swagger/index.html` через `swaggo/gin-swagger` и доступен только вне `release`-режима (в prod отключён). При изменении DTO или ручек спеку нужно перегенерировать. Краткий человекочитаемый справочник по auth-ручкам — в [api-auth.md](api-auth.md).

### Auth

| Метод | Путь | Описание |
|---|---|---|
| POST | `/auth/register` | `{email, password, display_name}` → создаёт user + wallet + бонус регистрации (транзакция `signup_bonus`, например 10 000 фантиков) |
| POST | `/auth/login` | `{email, password}` → `{access_token}` + refresh-cookie |
| POST | `/auth/refresh` | По refresh-cookie выдаёт новый access |
| POST | `/auth/logout` | Ревокация refresh-токена |
| GET | `/auth/:provider/login` | Редирект на Google/VK/Яндекс (provider ∈ google, vk, yandex) |
| GET | `/auth/:provider/callback` | Обмен кода, поиск/создание user по `auth_identities`, выдача токенов |

OAuth-логика: callback ищет `auth_identities(provider, provider_user_id)`. Нашли — логин. Не нашли, но email совпал с существующим user — привязываем identity. Иначе — регистрируем нового (с бонусом).

### Пользователи и кошелёк

| Метод | Путь | Описание |
|---|---|---|
| GET | `/me` | Профиль + баланс |
| PATCH | `/me` | Смена display_name / avatar |
| GET | `/me/transactions?page=` | История движений фантиков |
| GET | `/users/:id` | Публичный профиль: имя, дата регистрации, статистика (ставок всего, winrate, profit, ROI) |
| GET | `/users/:id/bets?status=&page=` | Публичная история ставок пользователя |

### События и ставки

| Метод | Путь | Описание |
|---|---|---|
| GET | `/sports` | Список видов спорта, по которым есть события в БД (+ 'custom') |
| GET | `/events?sport=&status=upcoming&page=&q=` | Лента событий с рынками и текущими коэффициентами |
| GET | `/events/:id` | Событие + markets + outcomes |
| POST | `/bets` | `{outcome_id, stake}` → создать ставку (см. флоу ниже) |
| GET | `/me/bets?status=&page=` | Мои ставки |
| GET | `/leaderboard?period=week\|month\|all&metric=profit\|roi&page=` | Топ прогнозистов |

### Admin (middleware: role = admin)

В M8 админка управляет тремя связками: **чемпионаты**, **спортивные матчи** (manual, ML+TOTALS, расчёт по счёту) и **произвольные события** (custom, расчёт по победителю). Также сохраняются корректировка баланса (M6).

#### Чемпионаты/лиги (M8)

| Метод | Путь | Описание |
|---|---|---|
| GET | `/admin/leagues` | Список чемпионатов (с фильтром по `sport_slug`) |
| POST | `/admin/leagues` | Создать: `{name, sport_slug}` |
| PATCH | `/admin/leagues/:id` | Переименовать / сменить спорт |
| DELETE | `/admin/leagues/:id` | Удалить, если нет привязанных событий (иначе 409) |

#### Спортивные матчи — source='manual' (M8)

Создаёт событие `source='manual'` с командами, ссылкой на `league_id` и рынками ML/TOTALS с коэффициентами. Расчёт — по введённому счёту (переиспользует `SettlementService.SettleEvent(settled, scores)`).

| Метод | Путь | Описание |
|---|---|---|
| POST | `/admin/matches` | Создать матч: `{title, league_id, starts_at, home, away, markets:[{type:ML\|TOTALS, line?, outcomes:[{code, label, odds}]}]}` |
| PATCH | `/admin/matches/:id` | Правка (title/starts_at/команды/коэффициенты) или отмена (`status:'cancelled'` → void ставок) |
| POST | `/admin/matches/:id/scores` | Ввести финальный счёт `{home, away}` → расчёт ML+TOTALS → `settled` |
| POST | `/admin/matches/:id/status` | Ручной перевод статуса `upcoming → live` (ставки закрываются), до ввода счёта |

#### Произвольные события — source='custom' (M6, остаётся)

| Метод | Путь | Описание |
|---|---|---|
| POST | `/admin/events` | Создать произвольное событие: `{title, starts_at, market: {question, outcomes: [{label, odds}]}}` |
| PATCH | `/admin/events/:id` | Редактировать / отменить (`status:'cancelled'` → void всех ставок с возвратом) |
| POST | `/admin/events/:id/settle` | `{winning_outcome_id}` — ручной расчёт по победившему исходу |
| POST | `/admin/users/:id/adjust` | `{amount, reason}` — ручная корректировка баланса |

> **Почему расчёт матчей — через ввод счёта, а не выбором исхода.** Матч имеет связанные рынки ML (победитель/ничья) и TOTALS (тотал). Введя `{home, away}`, сервис **автоматически** определяет победителя и тотал — не нужно отдельно закрывать каждый рынок. Это ровно тот код, что считал бы oddsapi-события (см. `SettlementService.buildPlan`).

### Флоу размещения ставки (самое важное место системы)

Одна транзакция БД:

```
BEGIN;
  1. SELECT outcome JOIN market JOIN event — проверить:
     event.status = 'upcoming' AND starts_at > now() AND market.status = 'open'
  2. SELECT * FROM wallets WHERE user_id = $1 FOR UPDATE;   -- блокировка от гонок
  3. Проверить balance >= stake (и stake в пределах min/max из конфига)
  4. INSERT INTO bets (..., odds = текущий outcome.odds,
                        potential_payout = floor(stake * odds));
  5. UPDATE wallets SET balance = balance - stake;
  6. INSERT INTO wallet_transactions (type='bet_stake', amount=-stake, ...);
COMMIT;
```

`SELECT ... FOR UPDATE` на кошельке — обязателен: без него два параллельных запроса могут увести баланс в минус. Тот же паттерн используется при выплатах и возвратах.

---

## 5. Интеграция с Odds-API.io: воркеры

> **Состояние: НА ПАУЗЕ (с M8).** Все три воркера ниже не запускаются, пока не задан `ODDS_API_KEY` (`cmd/server/main.go` регистрирует их только при непустом ключе). Код и клиент `internal/oddsapi` сохранены — при возврате к Odds-API достаточно снова прописать ключ. На время M8 источником событий, коэффициентов и результатов является админ через `/admin/*` (см. раздел 11 и веху M8).

Все воркеры — goroutines внутри того же процесса, расписание через `robfig/cron/v3`. Клиент `internal/oddsapi` — тонкая обёртка над REST с ретраями и логированием остатка лимита (заголовок `x-ratelimit-remaining`).

**EventSyncWorker (каждые 15 мин).** Для каждого спорта из конфига (начните с 1–2, например football и basketball): `GET /events?sport=...&status=pending,live` (горизонт по умолчанию 14 дней — для MVP достаточно). Upsert в `events` по `(source='oddsapi', external_id)`. Для новых событий создаёт markets ML/TOTALS с пустыми outcomes (заполнит OddsSync). Перевод `upcoming → live` по статусу из API.

**OddsSyncWorker (каждые 2–3 мин).** Берёт из БД события `status='upcoming'`, ближайшие N (например, стартующие в ближайшие 48 часов), пачками по 10 шлёт в `GET /odds/multi?eventIds=...&bookmakers=<один букмекер>` (1 пачка = 1 запрос к API). Обновляет `outcomes.odds` (для тоталов — выбирает основную линию, ближайшую к 1.90/1.90). Если букмекер убрал рынок — `market.status='suspended'`. События, уже начавшиеся, не обновляет (ставки на них всё равно закрыты).

Бюджет лимита: 30 пачек × 20 раз/час = 600 запросов/час на 300 событий — запас огромный.

**SettlementWorker (каждые 5 мин).** Берёт события `source='oddsapi'` со `status IN ('upcoming','live')` и `starts_at < now()`, запрашивает `GET /events/{id}` (или пачкой через `/events?status=settled`). Когда статус из API = `settled`:

```
для каждого market события:
  ML:     победитель из scores.home / scores.away (ничья → draw)
  TOTALS: (scores.home + scores.away) vs line; равенство линии → void (push)
помечаем outcomes.result, market.status='settled'
для каждой pending-ставки на эти outcomes — в транзакции:
  won  → wallet += potential_payout, tx type='bet_payout'
  lost → только статус
  void → wallet += stake, tx type='bet_refund'
event.status='settled', scores сохраняем в events.scores
```

Статус `cancelled` из API → void всех ставок с возвратом. Расчёт идемпотентен: обрабатываем только ставки в `pending`, повторный прогон ничего не ломает.

---

## 6. Авторизация: детали

- Пароли — bcrypt (cost 10+). Access JWT (HS256, секрет в env) живёт 15 минут, claims: `user_id`, `role`. Refresh — случайные 32 байта, в БД хэш, срок 30 дней, httpOnly+Secure cookie.
- OAuth через `github.com/markbates/goth`: Google и Yandex поддерживаются из коробки, для VK ID — провайдер на основе стандартного `golang.org/x/oauth2` (эндпоинты VK задаются вручную). Совет: в первой итерации сделайте **только email+пароль и Google**, VK/Яндекс добавьте отдельной задачей — каждый провайдер это регистрация приложения, ключи, нюансы callback'ов.
- Middleware: `AuthRequired` (парсит JWT, кладёт user в context), `AdminRequired` (поверх первого).

---

## 7. Фронтенд (React + TS + Vite)

Страницы MVP:

1. **/** — лента событий: фильтр по спорту/дате, карточки с коэффициентами, клик по коэффициенту открывает бет-слип (модал/сайдбар: сумма ставки, потенциальный выигрыш, кнопка).
2. **/events/:id** — страница события со всеми рынками.
3. **/login, /register** — формы + кнопки OAuth.
4. **/me/bets** — мои ставки (pending / settled), баланс, история транзакций.
5. **/users/:id** — публичный профиль: статистика + история ставок.
6. **/leaderboard** — таблица топа: фильтр period/metric.
7. **/admin** — список кастомных событий, форма создания, кнопка settle.

Технически: React Router, TanStack Query для запросов/кэша, состояние авторизации в контексте (access-токен в памяти, refresh — cookie). UI-кит — на ваш вкус (Mantine/MUI/shadcn) или Tailwind. В dev — Vite proxy на `:8080`; в prod nginx раздаёт статику и проксирует `/api`.

---

## 8. Роадмап — нарезка задач

**M0. Каркас (≈ неделя)**
1. Репозиторий, структура проекта, docker-compose (postgres), .env-конфиг.
2. golang-migrate + первые миграции (users, wallets, wallet_transactions, refresh_tokens, auth_identities).
3. Gin: health-check, middleware логирования, формат ошибок.

**M1. Auth + кошелёк**
4. Регистрация по email (bcrypt, бонус регистрации в одной транзакции), login, JWT + refresh.
5. Middleware AuthRequired/AdminRequired, GET /me, GET /me/transactions.
6. OAuth Google через goth.

**M2. События из Odds-API**
7. Миграции events/markets/outcomes. Клиент internal/oddsapi (events, odds/multi, ретраи, лог лимитов).
8. EventSyncWorker + OddsSyncWorker. GET /sports, /events, /events/:id.

**M3. Ставки**
9. POST /bets (транзакционный флоу с FOR UPDATE), GET /me/bets.
10. SettlementWorker: расчёт ML, потом TOTALS; обработка cancelled.

**M4. Фронтенд MVP**
11. Каркас SPA, авторизация, лента событий, бет-слип, мои ставки.

**M5. Социальная часть**
12. GET /users/:id (+статистика SQL-агрегатом), /users/:id/bets, /leaderboard (+ in-memory кэш 60 сек). Страницы профиля и лидерборда.

**M6. Кастомные события + админка**
13. Admin-эндпоинты: создание, settle, cancel, adjust. Страница /admin.

**M7. Полировка и деплой**
14. Rate-limit на API (например, ulule/limiter), валидация (validator/v10), графceful shutdown воркеров.
15. Dockerfile (multi-stage), nginx, HTTPS (caddy/certbot), бэкап Postgres (pg_dump по крону).

**M8. Ручное управление событиями (текущая веха)**
16. Чемпионаты: таблица `leagues`, CRUD `/admin/leagues`.
17. Спортивные матчи (`source='manual'`): создание с командами, лигой и рынками ML/TOTALS; ввод финального счёта → расчёт (переиспользуется `SettlementService.SettleEvent`). Статус `upcoming → live` вручную.
18. Произвольные события (`source='custom'`, из M6) — остаются как есть.
19. Пауза Odds-API: воркеры не стартуют без `ODDS_API_KEY` (уже сделано в `main.go`); код сохранён.

---

## 9. Что дальше (за рамками MVP, но архитектура готова)

- **Экспрессы:** таблица `bet_legs` (bet_id, outcome_id, odds), `bets.odds` = произведение. Сейчас одна ставка = один исход, миграция несложная.
- **Live-ставки:** WebSocket Odds-API (`channels=odds,scores,status`) вместо поллинга; на фронт — SSE/WS. Слой markets/outcomes не меняется.
- **Еженедельное пополнение / «банкротство»:** новый type транзакции + cron-воркер.
- **Больше рынков:** Handicap и т.д. — добавляются как новые `market.type` + правила в SettlementService.
- **Redis:** когда лидерборд и лента перестанут влезать в один Postgres — кэш и rate-limit переезжают туда.

## 10. Открытые вопросы (мои текущие допущения)

1. **Виды спорта на старте** — предположил футбол + баскетбол. Чем меньше, тем проще отладить settlement. В M8 список формирует админ при создании лиг/матчей.
2. **Букмекер-источник коэффициентов** — один, задаётся конфигом (Bet365 или Pinnacle). Актуально только при включённом Odds-API; в M8 коэффициенты вносит админ.
3. **Экономика:** бонус 10 000 фантиков, ставка min 10 / max 10 000 — цифры произвольные, вынесены в конфиг.
4. **Лидерборд:** метрика по умолчанию — profit за период; ROI как вторая. Нужен ли минимальный порог числа ставок для попадания в топ (например, ≥10)?

---

## 11. Ручное управление событиями (веха M8)

На время M8 админ заменяет собой Odds-API: он заводит чемпионаты, матчи и коэффициенты, вводит результаты. Эта секция описывает, **как именно** это устроено — переиспользование существующего кода делает веху небольшой по объёму.

### 11.1 Почему без нового «расчётного движка»

`SettlementService` уже реализован и **нейтрален к источнику события**:

- `SettleEvent(eventID, 'settled', scores)` считает ML (победитель из `home/away`, ничья → draw) и TOTALS (`home+away` vs `line`, равенство → void) по переданному `scores`. Это ровно то, что нужно для ручного матча после ввода счёта.
- `SettleCustomEvent(eventID, winningOutcomeID)` считает произвольное событие по выбранному победителю (используется в M6, остаётся без изменений).
- Оба метода идемпотентны и проводят выплаты/возвраты в транзакции с `FOR UPDATE`. Нового финансового кода писать не нужно.

Следовательно, задача M8 сводится к **созданию данных** (матч + рынки + исходы) и **запуску расчёта** по готовому счёту — без новых правил settlement.

### 11.2 Источники событий: три значения `source`

| source | Кто создаёт | Рынки | Расчёт |
|---|---|---|---|
| `oddsapi` | Воркер (на паузе) | ML, TOTALS | Воркер по `scores` из API |
| `manual` | Админ (`/admin/matches`) | ML, TOTALS (с кэфами) | Админ вводит счёт → `SettleEvent(settled, scores)` |
| `custom` | Админ (`/admin/events`, M6) | CUSTOM | Админ выбирает победителя → `SettleCustomEvent` |

### 11.3 Чемпионаты (`leagues`)

Чемпионат — справочник: `{id, name, sport_slug}`. Событие ссылается на него через `events.league_id`. Логика:
- При создании матча админ указывает `league_id` существующей лиги; `events.league_name` заполняется её копией (денормализация для ленты без join).
- Удаление лиги блокируется, если к ней привязаны события (`conflict`, 409).
- Публичная лента `/events?league_id=` фильтрует события по чемпионату (см. M8).

### 11.4 Жизненный цикл ручного матча

```
создание (upcoming) ──► [ручной перевод] ──► live ──► ввод счёта ──► settled
        │                      │                              (SettleEvent по scores)
        └──────────────────────┴───── cancel (PATCH status) ──► cancelled (void ставок)
```

- `upcoming`: рынки `open`, ставки принимаются. Минимум один рынок (ML) с заполненными исходами и коэффициентами.
- `live`: ручная отметка «матч идёт» — рынки переходят в `suspended`, новые ставки блокируются (проверка в `BettingService.PlaceBet` уже есть по `event.status`). До ввода счёта статус можно не менять — он нужен скорее для UI; ставки и так закрываются по `starts_at > now()`.
- `settled`: админ вводит `{home, away}` → `SettleEvent(settled, scores)` определяет победителя/тотал, выплачивает ставки.
- `cancelled`: через `PATCH /admin/matches/:id` (`status:'cancelled'`) → `SettleEvent(cancelled)` возвращает все ставки.

### 11.5 Что НЕ меняется в M8

- `BettingService.PlaceBet` — без правок: он проверяет `event.status='upcoming'`, `starts_at>now()`, `market.status='open'` и фиксирует коэффициент. Для ручных матчей эти условия работают как есть.
- `bets`, `wallets`, `wallet_transactions` — схема и логика выплат/возвратов те же.
- Лента `/events`, `/events/:id`, лидерборд, профиль — читают те же таблицы; матчи появляются автоматически, как только созданы.
- Пакет `internal/oddsapi` и воркеры `internal/worker` — остаются в коде, не вызываются. Возврат к Odds-API — отдельная задача после M8.

### 11.6 Возврат к Odds-API (после M8)

Поскольку схема и сервис расчёта не были изменены под ручное управление, повторное включение Odds-API сводится к: (1) прописать `ODDS_API_KEY`, (2) убедиться, что воркер EventSync создаёт/подтягивает `leagues` по `sport_slug`+`name` из API. Ручные (`source='manual'`) и произвольные (`source='custom'`) события остаются рядом с oddsapi-событиями без конфликтов — их разделяет поле `source`.
