# FanticBet — Архитектура MVP

Платформа ставок на виртуальную валюту («фантики») на реальные спортивные события (данные из Odds-API.io) и кастомные события, создаваемые админом. Документ рассчитан на одного разработчика и постепенное развитие: сначала минимальное ядро, потом наращивание.

---

## 1. Принципиальные решения

| Решение | Выбор | Почему |
|---|---|---|
| Бэкенд | Go + Gin, **монолит** | Один сервис, один деплой. Микросервисы для MVP — лишняя сложность |
| БД | PostgreSQL 16 | Транзакции критичны для кошелька и ставок; JSONB для счетов матчей |
| ORM / доступ к БД | GORM (или sqlc, если хочется ближе к SQL) | GORM быстрее для старта; миграции — через golang-migrate отдельно от GORM |
| Кэш | Без Redis в MVP | Кэш коэффициентов живёт прямо в Postgres (их пишет воркер). In-memory кэш для лидерборда |
| Интеграция с Odds-API | Фоновые воркеры (sync), **не** проксирование | Лимит 5000 req/час. Пользователи читают только локальную БД |
| Расчёт ставок | Автоматический по `scores` из API для спортивных событий; ручной для кастомных | Поле `scores.periods` покрывает рынки ML и Totals |
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
                │   └─ LeaderboardService                      │
                │                                              │
                │  Repository layer (Postgres)                 │
                │                                              │
                │  Background workers (goroutines + cron):     │
                │   ├─ EventSyncWorker   (каждые 15 мин)  ──┐  │
                │   ├─ OddsSyncWorker    (каждые 2 мин)   ──┼──┼──► Odds-API.io
                │   └─ SettlementWorker  (каждые 5 мин)   ──┘  │    (REST, apiKey)
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
│   ├── oddsapi/                  # HTTP-клиент Odds-API.io (свой пакет, легко мокается)
│   └── worker/                   # event_sync.go, odds_sync.go, settlement.go
├── migrations/                   # golang-migrate, *.up.sql / *.down.sql
├── web/                          # фронтенд (React, отдельный package.json)
├── docker-compose.yml
└── .env.example
```

---

## 3. Модель данных и схема БД

Ключевая идея: единая модель `events → markets → outcomes` и для спортивных, и для кастомных событий. Ставка ссылается на `outcome` и **фиксирует коэффициент на момент ставки** — коэффициенты в `outcomes` потом меняются воркером, но ставку это не затрагивает.

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

CREATE TABLE events (
    id           BIGSERIAL PRIMARY KEY,
    source       TEXT NOT NULL,                  -- 'oddsapi' | 'custom'
    external_id  BIGINT,                         -- id события в Odds-API (NULL для custom)
    sport_slug   TEXT NOT NULL,                  -- 'football', ... ; 'custom' для кастомных
    league_name  TEXT,
    title        TEXT NOT NULL,                  -- "Manchester United — Liverpool" или текст кастомного
    home         TEXT,                           -- NULL для кастомных
    away         TEXT,
    starts_at    TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'upcoming',
                  -- 'upcoming' | 'live' | 'settled' | 'cancelled'
    scores       JSONB,                          -- сырой scores из API; для аудита расчёта
    created_by   BIGINT REFERENCES users(id),    -- админ-автор кастомного события
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);
CREATE INDEX idx_events_status_start ON events(status, starts_at);

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
- Для событий из Odds-API храним **одного букмекера** (например, Pinnacle или Bet365 — задаётся конфигом) и два рынка: ML и основной тотал. Этого достаточно для MVP и резко упрощает синхронизацию.

---

## 4. REST API

Префикс `/api/v1`. Авторизация: `Authorization: Bearer <access JWT>`; refresh — httpOnly cookie. Ответы — JSON, ошибки в формате `{"error": {"code": "...", "message": "..."}}`.

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

| Метод | Путь | Описание |
|---|---|---|
| POST | `/admin/events` | Создать кастомное событие: `{title, starts_at, market: {question, outcomes: [{label, odds}]}}` |
| PATCH | `/admin/events/:id` | Редактировать / отменить (cancel → void всех ставок с возвратом) |
| POST | `/admin/events/:id/settle` | `{winning_outcome_id}` — ручной расчёт кастомного события |
| POST | `/admin/users/:id/adjust` | `{amount, reason}` — ручная корректировка баланса |

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

---

## 9. Что дальше (за рамками MVP, но архитектура готова)

- **Экспрессы:** таблица `bet_legs` (bet_id, outcome_id, odds), `bets.odds` = произведение. Сейчас одна ставка = один исход, миграция несложная.
- **Live-ставки:** WebSocket Odds-API (`channels=odds,scores,status`) вместо поллинга; на фронт — SSE/WS. Слой markets/outcomes не меняется.
- **Еженедельное пополнение / «банкротство»:** новый type транзакции + cron-воркер.
- **Больше рынков:** Handicap и т.д. — добавляются как новые `market.type` + правила в SettlementService.
- **Redis:** когда лидерборд и лента перестанут влезать в один Postgres — кэш и rate-limit переезжают туда.

## 10. Открытые вопросы (мои текущие допущения)

1. **Виды спорта на старте** — предположил футбол + баскетбол. Чем меньше, тем проще отладить settlement.
2. **Букмекер-источник коэффициентов** — один, задаётся конфигом (Bet365 или Pinnacle). Сравнение букмекеров пользователю не показываем.
3. **Экономика:** бонус 10 000 фантиков, ставка min 10 / max 10 000 — цифры произвольные, вынесены в конфиг.
4. **Лидерборд:** метрика по умолчанию — profit за период; ROI как вторая. Нужен ли минимальный порог числа ставок для попадания в топ (например, ≥10)?
