# FanticBet — Декомпозиция задач разработки

Подробная разбивка реализации MVP на мелкие задачи для самостоятельной разработки (соло или с ИИ). Порядок соответствует роадмапу из архитектуры (M0–M7), но каждая веха разбита на атомарные шаги. Задачи помечены чекбоксами для трекинга.

**Обозначения сложности:** 🟢 простая (≤ полдня) · 🟡 средняя (≈день) · 🔴 объёмная/рисковая (несколько подзадач).

**Допущения (можно пересмотреть):**
- OAuth в первой итерации — только Google; VK/Яндекс вынесены в backlog (см. M1-доп).
- Тестовые задачи помечены *(тест)* и опциональны, но рекомендованы для критичных мест (кошелёк, ставки, settlement).
- Старт по видам спорта: football + basketball; источник коэффициентов — один букмекер из конфига.

---

## M0. Каркас проекта

### Репозиторий и структура
- [x] 🟢 Создать git-репозиторий, добавить `.gitignore` для Go (+ `.env`, `web/node_modules`, бинарники)
- [x] 🟢 Инициализировать Go-модуль: `go mod init github.com/<you>/fanticbet`
- [x] 🟢 Создать дерево каталогов: `cmd/server/`, `internal/{config,domain,handler,service,repository,oddsapi,worker}/`, `migrations/`, `web/`
- [x] 🟢 Добавить `README.md` со стартовыми инструкциями (как поднять локально)

### Конфигурация и окружение
- [x] 🟢 Подключить базовые зависимости: `gin-gonic/gin`, драйвер БД (`jackc/pgx` или GORM), `joho/godotenv` (или `spf13/viper`)
- [x] 🟢 Создать `.env.example` и `.env` с ключами: `APP_PORT`, `DB_DSN`, `JWT_SECRET`, `ODDS_API_KEY`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `BOOKMAKER`, `SPORTS`, `SIGNUP_BONUS`, `BET_MIN`, `BET_MAX`
- [x] 🟢 Реализовать пакет `internal/config`: чтение env в типизированную структуру `Config` с дефолтами и валидацией обязательных полей

### База данных и Docker
- [x] 🟢 Написать `docker-compose.yml` с сервисом `postgres:16` (volume, порт, env)
- [ ] 🟢 Поднять Postgres локально, проверить подключение через `psql`/GUI
- [x] 🟢 Установить `golang-migrate` (CLI) и добавить Makefile-таргеты `migrate-up` / `migrate-down` / `migrate-new`
- [x] 🟢 Реализовать подключение к БД в коде (пул соединений) + `Ping()` при старте

### HTTP-каркас
- [x] 🟢 Написать `cmd/server/main.go`: загрузка конфига → подключение к БД → инициализация роутера Gin → запуск
- [x] 🟢 Добавить эндпоинт `GET /health` (проверка БД)
- [x] 🟢 Middleware логирования запросов (метод, путь, статус, latency)
- [x] 🟢 Хелпер единого формата ошибок: `{"error": {"code", "message"}}` + функции-обёртки
- [x] 🟢 Middleware CORS (для dev-фронта)
- [x] 🟡 Скелет graceful shutdown HTTP-сервера (перехват SIGINT/SIGTERM, `http.Server.Shutdown`)

---

## M1. Авторизация и кошелёк

### Миграции
- [x] 🟢 Миграция `users` (+ unique email)
- [x] 🟢 Миграция `auth_identities` (+ unique provider+provider_user_id)
- [x] 🟢 Миграция `refresh_tokens`
- [x] 🟢 Миграция `wallets` (+ CHECK balance >= 0)
- [x] 🟢 Миграция `wallet_transactions` (+ индекс по user_id, created_at)

### Domain-структуры
- [x] 🟢 Описать структуры: `User`, `Wallet`, `WalletTransaction`, `RefreshToken`, `AuthIdentity` в `internal/domain`
- [x] 🟢 Завести константы типов транзакций (`signup_bonus`, `bet_stake`, `bet_payout`, `bet_refund`, `admin_adjust`) и ролей (`user`, `admin`)

### Репозитории
- [x] 🟡 `UserRepository`: `Create`, `GetByID`, `GetByEmail`, `Update`
- [x] 🟡 `WalletRepository`: `Create`, `GetByUserIDForUpdate` (с `SELECT ... FOR UPDATE`), `UpdateBalance`
- [x] 🟢 `WalletTransactionRepository`: `Create`, `ListByUser` (пагинация)
- [x] 🟢 `RefreshTokenRepository`: `Create`, `GetByHash`, `Revoke`
- [x] 🟢 `AuthIdentityRepository`: `GetByProvider`, `Create`

### Криптография и токены
- [x] 🟢 Хелпер bcrypt: `HashPassword`, `CheckPassword` (cost ≥ 10)
- [x] 🟡 Генерация и парсинг access-JWT (HS256, claims `user_id`+`role`, TTL 15 мин)
- [x] 🟢 Генерация refresh-токена (32 случайных байта) + sha256-хэш для хранения

### Сервисы
- [x] 🔴 `AuthService.Register` — **в одной транзакции**: создать user → wallet → начислить `signup_bonus` (запись в `wallet_transactions` + баланс)
- [x] 🟡 `AuthService.Login`: проверка пароля → выдача access + создание refresh
- [x] 🟡 `AuthService.Refresh`: проверка refresh-хэша/срока/ревокации → новый access
- [x] 🟢 `AuthService.Logout`: ревокация refresh-токена
- [x] 🟢 `UserService.GetMe` (профиль + баланс одним ответом), `UpdateProfile` (display_name, avatar)

### Handlers и middleware
- [x] 🟢 `POST /auth/register` (+ валидация email/пароля через validator/v10)
- [x] 🟢 `POST /auth/login` (refresh в httpOnly+Secure cookie)
- [x] 🟢 `POST /auth/refresh` (читает cookie; для Postman — также из тела)
- [x] 🟢 `POST /auth/logout`
- [x] 🟡 Middleware `AuthRequired` (парсит JWT, кладёт user в context)
- [x] 🟢 Middleware `AdminRequired` (поверх `AuthRequired`, проверка role)
- [x] 🟢 `GET /me`
- [x] 🟢 `PATCH /me`
- [x] 🟢 `GET /me/transactions?page=`
- [x] 🟢 Swagger/OpenAPI: аннотации `swaggo` на хендлерах + UI на `/swagger/index.html` (генерация `swag init` → `docs/swagger`, `make swagger`)
- [ ] 🟡 *(тест)* Проверка регистрации: баланс кошелька = сумме транзакций после signup — *отложено: интеграционный тест handler→DB (testcontainers), либо ручная проверка через Postman*

### OAuth (Яндекс + VK)
- [x] 🟢 Зарегистрировать OAuth-приложения в Яндекс ID и VK, получить client_id/secret, прописать redirect URI
- [x] 🟡 Реализовать OAuth через `golang.org/x/oauth2` для Яндекс и VK (без `markbates/goth` — единый стек для обоих провайдеров)
- [x] 🟢 `GET /auth/:provider/login` (редирект к провайдеру, CSRF-state в cookie)
- [x] 🔴 `GET /auth/:provider/callback`: проверка state → обмен кода → профиль от провайдера → поиск `auth_identities` → (логин / привязка по email / регистрация с бонусом) → выдача токенов
- [x] 🟢 Пользователи без email (VK не дал) — допускаются (OAuth-only аккаунт)

### OAuth Google (backlog)
- [ ] 🟢 Зарегистрировать OAuth-приложение в Google Cloud, получить client_id/secret
- [ ] 🟡 Подключить провайдер Google (через `golang.org/x/oauth2`, по аналогии с Яндекс/VK)

### Backlog M1 (после MVP-ядра)

---

## M2. События из Odds-API

### Миграции и domain
- [x] 🟢 Миграция `events` (+ unique source+external_id, индекс status+starts_at)
- [x] 🟢 Миграция `markets` (+ индекс event_id)
- [x] 🟢 Миграция `outcomes` (+ индекс market_id, CHECK odds > 1.0)
- [x] 🟢 Domain-структуры `Event`, `Market`, `Outcome` + константы (типы рынков `ML`/`TOTALS`/`CUSTOM`, статусы)

### Клиент Odds-API (`internal/oddsapi`)
- [x] 🟢 Изучить документацию API (загрузить `https://docs.odds-api.io/llms-full.txt`), зафиксировать формат ответов events/odds/scores
- [x] 🟢 Базовый HTTP-клиент с базовым URL и apiKey в query, общий метод `do()`
- [x] 🟢 Метод `GetSports()`
- [x] 🟡 Метод `GetEvents(sport, status)` + маппинг ответа в domain
- [x] 🟡 Метод `GetOddsMulti(eventIds []int, bookmakers)` (до 10 событий за запрос)
- [x] 🟢 Метод `GetEvent(id)` (для settlement, чтение `scores`)
- [x] 🟡 Ретраи с backoff на 5xx/таймауты
- [x] 🟢 Логирование остатка лимита из заголовка `x-ratelimit-remaining`

### Репозитории
- [x] 🟡 `EventRepository`: `Upsert` (по source+external_id), `GetByID`, `ListWithFilters` (sport/status/q/page), `ListForOddsSync` (upcoming, старт ≤ 48ч), `ListForSettlement` (upcoming/live, started), `UpdateStatusAndScores`
- [x] 🟢 `MarketRepository`: `CreateForEvent`, `GetByEvent`, `UpdateStatus`
- [x] 🟢 `OutcomeRepository`: `Upsert`, `GetByMarket`, `UpdateOdds`, `UpdateResult`

### Воркеры
- [x] 🟢 Подключить `robfig/cron/v3`, общий раннер воркеров (расписания из конфига; `SkipIfStillRunning`; ctx с таймаутом на итерацию)
- [x] 🔴 `EventSyncWorker` (раз в час — бесплатный тариф API): по спортам из конфига → `GetEvents` → upsert событий → для новых создать markets ML/TOTALS с пустыми outcomes → перевод upcoming→live по статусу API
- [x] 🔴 `OddsSyncWorker` (раз в 30 мин — бесплатный тариф API): выбрать события из БД → пачки по 10 → `GetOddsMulti` → обновить odds; для TOTALS выбрать основную линию (ближайшую к 1.90/1.90); снятый рынок → `suspended`; начавшиеся не трогать
- [x] 🟡 Подключить воркеры в `main`, корректная остановка по shutdown-сигналу

### Handlers
- [ ] 🟢 `GET /sports` (виды спорта из БД + `custom`)
- [ ] 🟡 `GET /events?sport=&status=&page=&q=` (лента с рынками и текущими коэффициентами)
- [ ] 🟢 `GET /events/:id` (событие + markets + outcomes)

---

## M3. Ставки и расчёт

### Миграция и domain
- [ ] 🟢 Миграция `bets` (+ индексы user/created_at и частичный по event WHERE status='pending')
- [ ] 🟢 Domain-структура `Bet` + константы статусов
- [ ] 🟢 Вынести `BET_MIN`/`BET_MAX` в конфиг

### Репозиторий
- [ ] 🟡 `BetRepository`: `Create`, `GetByID`, `ListByUser` (status/page), `ListPendingByOutcomes`, `UpdateStatusSettled`

### Размещение ставки
- [ ] 🔴 `BettingService.PlaceBet` — **одна транзакция**:
  1. загрузить outcome→market→event, проверить `event.status='upcoming'`, `starts_at>now()`, `market.status='open'`
  2. `wallet FOR UPDATE`
  3. проверить `balance ≥ stake` и `stake ∈ [min,max]`
  4. `INSERT bet` (odds = текущий outcome.odds, `potential_payout = floor(stake*odds)`)
  5. списать с кошелька + `wallet_transactions(type='bet_stake', amount=-stake)`
- [ ] 🟢 `POST /bets` (`{outcome_id, stake}`) + валидация
- [ ] 🟢 `GET /me/bets?status=&page=`
- [ ] 🟡 *(тест)* Конкурентная ставка: 2 параллельных запроса не уводят баланс в минус (проверка `FOR UPDATE`)

### Settlement
- [ ] 🟡 `SettlementService`: расчёт **ML** (победитель из `scores.home/away`, ничья → draw)
- [ ] 🟡 `SettlementService`: расчёт **TOTALS** (`home+away` vs `line`; равенство линии → void/push)
- [ ] 🔴 `SettlementService.SettleEvent` — пометить outcomes.result, market.status='settled', затем по каждой pending-ставке **в транзакции**: won → payout + tx `bet_payout`; lost → только статус; void → refund stake + tx `bet_refund`; в конце event.status='settled', сохранить `scores`
- [ ] 🟡 Обработка статуса `cancelled` из API → void всех ставок события с возвратом
- [ ] 🟡 Обеспечить идемпотентность (обрабатывать только `pending`-ставки; повторный прогон безопасен)
- [ ] 🟡 `SettlementWorker` (каждые 5 мин): выбрать started-события oddsapi → `GetEvent` → при `settled`/`cancelled` вызвать сервис
- [ ] 🟢 Подключить settlement-воркер в раннер
- [ ] 🟡 *(тест)* Полный цикл: ставка → settlement won/lost/void → корректные баланс и транзакции

---

## M4. Фронтенд MVP (React + TS + Vite)

### Каркас
- [ ] 🟢 Инициализировать `web/` через Vite (react-ts), почистить шаблон
- [ ] 🟢 Подключить React Router, описать роуты-заглушки всех страниц
- [ ] 🟢 Подключить TanStack Query (QueryClientProvider)
- [ ] 🟢 Выбрать UI-кит (Mantine/MUI/shadcn/Tailwind) и подключить
- [ ] 🟢 Настроить Vite dev-proxy на `:8080` для `/api`
- [ ] 🟡 API-клиент: fetch-обёртка, добавление Bearer, авто-refresh на 401
- [ ] 🟡 Контекст авторизации (access-токен в памяти, refresh — cookie), хуки `useAuth`
- [ ] 🟢 Общий layout: шапка, навигация, отображение баланса для залогиненного

### Страницы
- [ ] 🟢 `/login` — форма + кнопка «Войти через Google»
- [ ] 🟢 `/register` — форма регистрации
- [ ] 🟡 `/` — лента событий: фильтр по спорту/дате, карточки с коэффициентами
- [ ] 🔴 Бет-слип (модал/сайдбар): сумма ставки, расчёт потенциального выигрыша, отправка `POST /bets`, обработка ошибок (баланс, лимиты, закрыт рынок)
- [ ] 🟡 `/events/:id` — событие со всеми рынками
- [ ] 🟡 `/me/bets` — мои ставки (pending/settled), баланс, история транзакций
- [ ] 🟢 Обработка состояний загрузки/ошибок/пустых списков (общие компоненты)

---

## M5. Социальная часть

### Бэкенд
- [ ] 🟡 SQL-агрегат статистики пользователя: всего ставок, winrate, profit (Σpayout−Σstake), ROI
- [ ] 🟢 `GET /users/:id` (публичный профиль + статистика)
- [ ] 🟢 `GET /users/:id/bets?status=&page=` (публичная история)
- [ ] 🔴 SQL-запрос лидерборда: period (week/month/all) × metric (profit/roi), пагинация; решить вопрос мин. порога числа ставок (напр. ≥10)
- [ ] 🟡 In-memory кэш лидерборда (TTL ~60 сек) с ключом по period+metric
- [ ] 🟢 `GET /leaderboard?period=&metric=&page=`

### Фронтенд
- [ ] 🟡 Страница `/users/:id` — статистика + история ставок
- [ ] 🟡 Страница `/leaderboard` — таблица с фильтрами period/metric

---

## M6. Кастомные события и админка

### Бэкенд
- [ ] 🟡 `AdminService.CreateCustomEvent` (event source='custom' + market CUSTOM + outcomes из запроса)
- [ ] 🟢 `POST /admin/events` (`{title, starts_at, market:{question, outcomes:[{label,odds}]}}`)
- [ ] 🟡 `AdminService.EditEvent` / `CancelEvent` (cancel → void всех ставок с возвратом)
- [ ] 🟢 `PATCH /admin/events/:id`
- [ ] 🟡 `AdminService.SettleCustom` (`{winning_outcome_id}` → расчёт ставок как won/lost)
- [ ] 🟢 `POST /admin/events/:id/settle`
- [ ] 🟡 `AdminService.AdjustBalance` (`{amount, reason}` + tx `admin_adjust`, в транзакции с FOR UPDATE)
- [ ] 🟢 `POST /admin/users/:id/adjust`

### Фронтенд
- [ ] 🟡 Страница `/admin`: список кастомных событий, форма создания, кнопка settle/cancel, форма adjust

---

## M7. Полировка и деплой

### Надёжность
- [ ] 🟡 Rate-limit middleware на API (`ulule/limiter` или аналог)
- [ ] 🟢 Полная валидация входных DTO (`go-playground/validator`)
- [ ] 🟡 Доделать graceful shutdown воркеров (дождаться текущих итераций, контекст с отменой)
- [ ] 🟢 Структурированное логирование (slog/zerolog), уровни, request-id

### Деплой
- [ ] 🟡 `Dockerfile` (multi-stage build Go)
- [ ] 🟢 Сборка фронта в prod, отдача статики
- [ ] 🟡 `nginx`-конфиг: статика + проксирование `/api`
- [ ] 🟡 Полный `docker-compose.yml` для prod (app + postgres + nginx)
- [ ] 🟡 HTTPS (Caddy или certbot/Let's Encrypt)
- [ ] 🟢 Бэкап Postgres по крону (`pg_dump`)
- [ ] 🟢 Деплой на VPS, smoke-тест всех ключевых сценариев
- [ ] 🟢 Финальный `README` (запуск dev/prod, миграции, переменные окружения)

---

## Backlog (за рамками MVP — архитектура готова)

- [ ] Экспрессы: таблица `bet_legs`, `bets.odds` = произведение
- [ ] Live-ставки: WebSocket Odds-API (`channels=odds,scores,status`) + SSE/WS на фронт
- [ ] Еженедельное пополнение / «банкротство»: новый тип транзакции + cron-воркер
- [ ] Новые рынки (Handicap и т.д.): новый `market.type` + правила в SettlementService
- [ ] Redis: кэш лидерборда/ленты + rate-limit
- [ ] Флаг приватности истории ставок (`users.is_history_public`)
- [ ] Материализованный лидерборд (`leaderboard_snapshots`) при росте нагрузки

---

## Открытые вопросы к решению до/во время разработки

1. Минимальный порог числа ставок для попадания в лидерборд (например, ≥10)?
2. Какой именно букмекер-источник (Bet365 / Pinnacle) и горизонт синхронизации событий (по умолчанию 14 дней)?
3. Окончательные экономические параметры: бонус регистрации, min/max ставки.
4. Сколько видов спорта на старте (рекомендация — 1–2 для упрощения отладки settlement).
