# FanticBet — фронтенд (web)

SPA на React + TypeScript + Vite. Каркас вехи M4.

## Стек

- **React 19 + TypeScript**
- **Vite** — сборка и dev-сервер (с proxy `/api` → `:8080`)
- **React Router** — маршрутизация
- **TanStack Query** — запросы и кэш к REST API
- **Собственная дизайн-система** на CSS-переменных по макету
  `docs/fanticbet-design` (светлая/тёмная тема, без UI-кита)

## Запуск (dev)

```bash
cd web
npm install
npm run dev        # http://localhost:5173, /api проксируется на :8080
```

Перед стартом фронта поднимите бэкенд (`go run cmd/server/main.go`) и Postgres
(`docker compose up -d postgres`).

## Скрипты

| Команда | Действие |
|---|---|
| `npm run dev` | dev-сервер с HMR |
| `npm run build` | типизация (`tsc -b`) + прод-сборка в `dist/` |
| `npm run preview` | предпросмотр прод-сборки |
| `npm run typecheck` | только проверка типов |

## Структура

```
src/
├── index.css     # дизайн-система: токены (CSS-переменные), темы, классы компонентов
├── api/          # API-клиент (Bearer + авто-refresh) и обёртки эндпоинтов
│   ├── client.ts # fetch-обёртка, единый формат ошибок, авто-refresh на 401
│   ├── types.ts  # типы ответов (зеркало DTO бэкенда)
│   ├── auth.ts   # login / register / logout / getMe / oauth
│   ├── events.ts # sports / events
│   ├── bets.ts   # placeBet / my bets
│   └── user.ts   # профиль / транзакции
├── context/      # Auth, Theme (тема + localStorage), Toast, Betslip (купон)
├── hooks/        # useAuth / useTheme / useToast / useBetslip
├── lib/          # format (фантики/даты/коэф.), labels (рынки/спорт), bet (VM статусов)
├── components/   # Layout, Header, MobileNav, Betslip, OutcomeButton, состояния
└── pages/        # экраны (часть — заглушки под M5/M6)
```

## Дизайн-система и темы

Перенос макета из `docs/fanticbet-design`. Все цвета — CSS-переменные на
`:root` (и `:root[data-theme="dark"]`); тема переключается в шапке и хранится в
`localStorage` (`fb-theme`). Компонентные классы — с префиксом `.fb-*`.

## Купон ставок

`BetslipContext` хранит выбранные исходы (один исход на событие). Кнопка
коэффициента (`OutcomeButton`) добавляет/убирает позицию. «Сделать ставку»
отправляет каждую позицию отдельным `POST /bets`, затем обновляет баланс и
историю; ошибки бэкенда (недостаточно средств, лимиты, закрытый рынок)
показываются тостом.

## Авторизация

Access-токен хранится только в памяти (модуль `api/client.ts`), refresh-токен —
в httpOnly-cookie (ставит бэкенд). При 401 клиент один раз дёргает
`POST /auth/refresh` и повторяет запрос; если refresh не удался — сессия
сбрасывается (`AuthContext`). На старте приложения `getMe()` инициирует
silent-refresh, восстанавливая сессию по cookie.
