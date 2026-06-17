# FanticBet

Платформа ставок на виртуальную валюту («фантики») на реальные спортивные события.

## Стек

- **Бэкенд:** Go + Gin + PostgreSQL
- **Фронтенд:** React + TypeScript + Vite (папка `web/`)
- **Миграции:** golang-migrate (SQL)
- **Деплой:** Docker Compose

## Быстрый старт

### 1. Запустить PostgreSQL

```bash
docker compose up -d postgres
```

### 2. Создать .env

```bash
cp .env.example .env
# Заполните ODDS_API_KEY и Google OAuth ключи
```

### 3. Применить миграции

```bash
make migrate-up
```

### 4. Запустить сервер

```bash
go run cmd/server/main.go
```

Сервер запустится на `http://localhost:8080`.

Проверка: `GET http://localhost:8080/health`

## Полезные команды

```bash
make build          # сборка бинаря
make migrate-new    # создать новую миграцию (будет спросить имя)
make migrate-up     # применить миграции
make migrate-down   # откатить миграции
make docker-down    # остановить PostgreSQL
```

## Структура проекта

```
cmd/server/     — точка входа
internal/
  config/       — конфигурация (env)
  domain/       — структуры данных
  handler/      — HTTP-хендлеры + middleware
  service/      — бизнес-логика
  repository/   — работа с БД
  oddsapi/      — клиент Odds-API.io
  worker/       — фоновые воркеры
migrations/     — SQL-миграции
web/            — React-фронтенд
docs/           — документация
```

Подробная архитектура: [docs/architecture.md](docs/architecture.md)
Задачи по вехам: [docs/tasks.md](docs/tasks.md)
