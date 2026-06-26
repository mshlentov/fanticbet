# FanticBet — Декомпозиция задач разработки

Подробная разбивка реализации MVP на мелкие задачи для самостоятельной разработки (соло или с ИИ). Порядок соответствует роадмапу из архитектуры (M0–M7), но каждая веха разбита на атомарные шаги. Задачи помечены чекбоксами для трекинга.

**Обозначения сложности:** 🟢 простая (≤ полдня) · 🟡 средняя (≈день) · 🔴 объёмная/рисковая (несколько подзадач).

**Допущения (можно пересмотреть):**
- OAuth в первой итерации — только Google; VK/Яндекс вынесены в backlog (см. M1-доп).
- Тестовые задачи помечены *(тест)* и опциональны, но рекомендованы для критичных мест (кошелёк, ставки, settlement).
- Старт по видам спорта: football + basketball; источник коэффициентов — один букмекер из конфига.
- **Текущая веха — M8 «Ручное управление событиями»**: интеграция с Odds-API временно приостановлена (воркеры не стартуют без `ODDS_API_KEY`), события и коэффициенты вводит админ. См. архитектуру §11 и веху M8 ниже.

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
- [x] 🟢 `GET /sports` (виды спорта из БД + `custom`)
- [x] 🟡 `GET /events?sport=&status=&page=&q=` (лента с рынками и текущими коэффициентами)
- [x] 🟢 `GET /events/:id` (событие + markets + outcomes)

---

## M3. Ставки и расчёт

### Миграция и domain
- [x] 🟢 Миграция `bets` (+ индексы user/created_at и частичный по event WHERE status='pending')
- [x] 🟢 Domain-структура `Bet` + константы статусов
- [x] 🟢 Вынести `BET_MIN`/`BET_MAX` в конфиг

### Репозиторий
- [x] 🟡 `BetRepository`: `Create`, `GetByID`, `ListByUser` (status/page), `ListPendingByOutcomes`, `UpdateStatusSettled`

### Размещение ставки
- [x] 🔴 `BettingService.PlaceBet` — **одна транзакция**:
  1. загрузить outcome→market→event, проверить `event.status='upcoming'`, `starts_at>now()`, `market.status='open'`
  2. `wallet FOR UPDATE`
  3. проверить `balance ≥ stake` и `stake ∈ [min,max]`
  4. `INSERT bet` (odds = текущий outcome.odds, `potential_payout = floor(stake*odds)`)
  5. списать с кошелька + `wallet_transactions(type='bet_stake', amount=-stake)`
- [x] 🟢 `POST /bets` (`{outcome_id, stake}`) + валидация
- [x] 🟢 `GET /me/bets?status=&page=`
- [ ] 🟡 *(тест)* Конкурентная ставка: 2 параллельных запроса не уводят баланс в минус (проверка `FOR UPDATE`)

### Settlement
- [x] 🟡 `SettlementService`: расчёт **ML** (победитель из `scores.home/away`, ничья → draw)
- [x] 🟡 `SettlementService`: расчёт **TOTALS** (`home+away` vs `line`; равенство линии → void/push)
- [x] 🔴 `SettlementService.SettleEvent` — пометить outcomes.result, market.status='settled', затем по каждой pending-ставке **в транзакции**: won → payout + tx `bet_payout`; lost → только статус; void → refund stake + tx `bet_refund`; в конце event.status='settled', сохранить `scores`
- [x] 🟡 Обработка статуса `cancelled` из API → void всех ставок события с возвратом
- [x] 🟡 Обеспечить идемпотентность (обрабатывать только `pending`-ставки; повторный прогон безопасен)
- [x] 🟡 `SettlementWorker` (каждые 5 мин): выбрать started-события oddsapi → `GetEvent` → при `settled`/`cancelled` вызвать сервис
- [x] 🟢 Подключить settlement-воркер в раннер
- [x] 🟡 *(тест)* Полный цикл: ставка → settlement won/lost/void → корректные баланс и транзакции

---

## M4. Фронтенд MVP (React + TS + Vite)

### Каркас
- [x] 🟢 Инициализировать `web/` через Vite (react-ts), почистить шаблон
- [x] 🟢 Подключить React Router, описать роуты-заглушки всех страниц
- [x] 🟢 Подключить TanStack Query (QueryClientProvider)
- [x] 🟢 Выбрать UI-кит — **собственная дизайн-система** по макету `docs/fanticbet-design` (CSS-переменные, светлая/тёмная тема)
- [x] 🟢 Настроить Vite dev-proxy на `:8080` для `/api`
- [x] 🟡 API-клиент: fetch-обёртка, добавление Bearer, авто-refresh на 401
- [x] 🟡 Контекст авторизации (access-токен в памяти, refresh — cookie), хуки `useAuth`
- [x] 🟢 Общий layout: шапка, навигация, баланс, переключатель темы, мобильная навигация, тосты

### Страницы (по макету `docs/fanticbet-design/project/FanticBet.dc.html`)
- [x] 🟢 `/login` — форма + OAuth-кнопки (Яндекс/VK; Google — backlog)
- [x] 🟢 `/register` — форма регистрации
- [x] 🟡 `/` — лента событий: фильтр по спорту и статусу, карточки с рынками и коэффициентами *(фильтр по дате — позже)*
- [x] 🔴 Бет-слип (сайдбар/нижний лист): сумма, потенциальный выигрыш, `POST /bets` по каждой позиции, тосты на ошибки (баланс/лимиты/закрыт рынок)
- [x] 🟡 `/events/:id` — событие со всеми рынками + «мои ставки на это событие»
- [x] 🟡 `/me/bets` — баланс, статистика (винрейт/прибыль/ROI), вкладки активные/завершённые/транзакции
- [x] 🟢 Обработка состояний загрузки/ошибок/пустых списков (общие компоненты)

> **Зависит от бэкенда (для полноты экранов):** DTO ставки (`GET /me/bets`) не содержит названия события/исхода и рынка — на «Моих ставках» показываем `Событие #id` / `Исход #id`; `eventDTO` не отдаёт `scores`, поэтому в карточке события счёт не выводится. Социальные экраны (`/leaderboard`, `/users/:id`) и админка — заглушки в стиле макета до M5/M6.

---

## M5. Социальная часть

### Бэкенд
- [x] 🟡 SQL-агрегат статистики пользователя: всего ставок, winrate, profit (Σpayout−Σstake), ROI
- [x] 🟢 `GET /users/:id` (публичный профиль + статистика)
- [x] 🟢 `GET /users/:id/bets?status=&page=` (публичная история)
- [x] 🔴 SQL-запрос лидерборда: period (week/month/all) × metric (profit/roi), пагинация; решить вопрос мин. порога числа ставок (напр. ≥10)
- [x] 🟡 In-memory кэш лидерборда (TTL ~60 сек) с ключом по period+metric
- [x] 🟢 `GET /leaderboard?period=&metric=&page=`

### Фронтенд
- [x] 🟡 Страница `/users/:id` — статистика + история ставок
- [x] 🟡 Страница `/leaderboard` — таблица с фильтрами period/metric

---

## M6. Кастомные события и админка

### Бэкенд
- [x] 🟡 `AdminService.CreateCustomEvent` (event source='custom' + market CUSTOM + outcomes из запроса)
- [x] 🟢 `POST /admin/events` (`{title, starts_at, market:{question, outcomes:[{label,odds}]}}`)
- [x] 🟡 `AdminService.EditEvent` / `CancelEvent` (cancel → void всех ставок с возвратом)
- [x] 🟢 `PATCH /admin/events/:id`
- [x] 🟡 `AdminService.SettleCustom` (`{winning_outcome_id}` → расчёт ставок как won/lost)
- [x] 🟢 `POST /admin/events/:id/settle`
- [x] 🟡 `AdminService.AdjustBalance` (`{amount, reason}` + tx `admin_adjust`, в транзакции с FOR UPDATE)
- [x] 🟢 `POST /admin/users/:id/adjust`

### Фронтенд
- [x] 🟡 Страница `/admin`: список кастомных событий, форма создания, кнопка settle/cancel, форма adjust

---

## M8. Ручное управление событиями (текущая веха)

**Контекст.** Odds-API временно отключён (воркеры не стартуют без `ODDS_API_KEY` — уже сделано в `cmd/server/main.go`). Источником событий, коэффициентов и результатов становится админ. Архитектура и схема `events → markets → outcomes` не меняются; `SettlementService.SettleEvent(settled, scores)` уже умеет считать ML и TOTALS по счёту — переиспользуется без правок (см. `docs/architecture.md` §11). Веха разбита на три блока: чемпионаты, спортивные матчи и пауза Odds-API.

### Миграции
- [x] 🟢 Миграция `leagues` (`id, name, sport_slug, created_at, updated_at`) + индекс по `sport_slug` — миграция `000011_create_leagues`
- [x] 🟢 Миграция `events`: добавить колонки `league_id BIGINT REFERENCES leagues(id)` (NULLable) + индекс по `league_id`; `league_name` остаётся (денормализация). Новое значение `source='manual'` — без миграции (TEXT). — миграция `000012_add_events_league_id`
- [x] 🟢 Down-миграции для обеих (откат `league_id`/индекса и таблицы `leagues`)

### Domain и константы
- [x] 🟢 Domain `League { ID, Name, SportSlug, CreatedAt, UpdatedAt }` в `internal/domain`
- [x] 🟢 Константа `SourceManual EventSource = "manual"` рядом с `SourceOddsAPI`/`SourceCustom`

### Чемпионаты (leagues)
- [ ] 🟡 `LeagueRepository`: `Create`, `GetByID`, `List(sportSlug)`, `Update`, `Delete` (проверка «нет привязанных событий» — на уровне репозитория или сервиса)
- [ ] 🟡 `AdminService`: методы `CreateLeague` / `UpdateLeague` / `DeleteLeague` (с проверкой ссылок из `events.league_id` → 409 при наличии) / `ListLeagues`
- [ ] 🟢 Handlers: `GET /admin/leagues` (с фильтром `sport_slug`), `POST /admin/leagues`, `PATCH /admin/leagues/:id`, `DELETE /admin/leagues/:id` + DTO с валидацией
- [ ] 🟢 Публичный `GET /leagues?sport_slug=` (для фильтра в ленте) — опционально отдельной задачей

### Спортивные матчи (source='manual')
- [ ] 🔴 `AdminService.CreateMatch` — **одна транзакция**: создать `event` (`source='manual'`, `home/away`, `league_id`+`league_name`, `sport_slug`, `starts_at`, `status='upcoming'`) → рынки (`ML` обязателен; `TOTALS` с `line`) → исходы (`home/draw/away` для ML; `over/under` для TOTALS) с коэффициентами. Валидация: ≥1 рынок ML, кэфы > 1.0, `league_id` существует.
- [ ] 🟡 `AdminService.EditMatch` — правка `title/starts_at/home/away/league_id` и коэффициентов исходов; только для `source='manual'` и `status='upcoming'`
- [ ] 🟡 `AdminService.CancelMatch` — делегирует в `SettlementService.SettleEvent(cancelled)` (void ставок), как `CancelEvent` в M6
- [ ] 🔴 `AdminService.SetMatchScores(home, away)` — главное отличие от custom: вызывает `events.UpdateStatusAndScores` + `SettlementService.SettleEvent(eventID, 'settled', {home,away})`. Авторасчёт ML+TOTALS по счёту. Только для `source='manual'`, `status IN (upcoming, live)`, счёт ещё не введён.
- [ ] 🟢 `AdminService.SetMatchStatus('live')` — ручной перевод `upcoming → live` (рынки → `suspended`). До ввода счёта.
- [ ] 🟢 Handlers: `POST /admin/matches`, `PATCH /admin/matches/:id`, `POST /admin/matches/:id/scores`, `POST /admin/matches/:id/status` + DTO
- [ ] 🟢 Маршруты в `main.go` под группой `/admin` (за `AuthRequired`+`AdminRequired`)

### Лента событий (правки)
- [ ] 🟡 `GET /events`: добавить фильтр `league_id` (в `EventRepository.ListWithFilters`). Матчи `source='manual'` уже видны — отдельной выборки не нужно.
- [ ] 🟢 Swagger-аннотации на новых admin-эндпоинтах; перегенерация спеки (`swag init`)

### Пауза Odds-API
- [x] 🟢 Воркеры не стартуют без `ODDS_API_KEY` (уже реализовано в `cmd/server/main.go`)
- [ ] 🟢 Проверить, что при пустом ключе приложение стартует без ошибок и логирует пропуск воркеров (smoke-тест)

### Фронтенд
- [ ] 🟡 Раздел админки: CRUD чемпионатов
- [ ] 🔴 Форма создания матча (команды, лига, рынки ML/TOTALS с кэфами) + список матчей + ввод счёта → кнопка «рассчитать»
- [ ] 🟡 Фильтр ленты по чемпионату (`/events?league_id=`)
- [ ] 🟡 *(тест)* Полный цикл: создать матч → поставить → ввести счёт → сверить выплаты (won/lost/void) по ML и TOTALS
- [ ] 🟡 *(тест)* Отмена матча до ввода счёта → возврат всех ставок

### Тесты (критичные места)
- [ ] 🟡 *(тест)* `SetMatchScores` корректно считает ML (победа/ничья) и TOTALS (over/under/push) через переиспользуемый `SettlementService`
- [ ] 🟢 *(тест)* Нельзя удалить лигу с привязанными событиями (409)

### Backlog M8 (после основной части)
- [ ] 🟢 Загрузка коэффициентов сразу для нескольких событий (массовый ввод в админке)
- [ ] 🟢 Редактирование коэффициентов после `starts_at` (закрытие рынка вручную → `suspended`)
- [ ] 🟢 Дедлайн ставок по матчу: разрешать/запрещать ставки после `starts_at` через отдельный флаг (сейчас `BettingService` проверяет `starts_at > now()`)

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

- [ ] **Возврат к Odds-API** (после M8): прописать `ODDS_API_KEY`; доработать `EventSyncWorker`, чтобы он создавал/подтягивал `leagues` по `sport_slug`+`name` из ответа API и проставлял `events.league_id`. Схема и `SettlementService` уже поддерживают это (см. архитектуру §11.6).
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
