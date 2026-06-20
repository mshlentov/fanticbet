.PHONY: run build swagger migrate-up migrate-down migrate-new docker-up docker-down docker-logs

# ─── Запуск ────────────────────────────────────────────

run:
	go run cmd/server/main.go

build:
	go build -o bin/server cmd/server/main.go

# ─── Swagger ───────────────────────────────────────────
# Перегенерация OpenAPI-спеки в docs/swagger из аннотаций в коде.
# Требуется swag CLI: go install github.com/swaggo/swag/cmd/swag@latest
# UI доступен на http://localhost:8080/swagger/index.html (не в release-режиме).

swagger:
	swag init -g cmd/server/main.go -o docs/swagger --parseInternal

# ─── Миграции (требуется golang-migrate CLI) ───────────
# Установка: https://github.com/golang-migrate/migrate/tree/master/cmd/migrate

MIGRATE_URL ?= postgres://fanticbet:fanticbet@localhost:5432/fanticbet?sslmode=disable
MIGRATE_PATH = file://migrations

migrate-up:
	migrate -source $(MIGRATE_PATH) -database $(MIGRATE_URL) up

migrate-down:
	migrate -source $(MIGRATE_PATH) -database $(MIGRATE_URL) down

migrate-new:
	@read -p "Migration name: " name && \
	migrate create -ext sql -dir migrations -seq $$name

# ─── Docker ────────────────────────────────────────────

docker-up:
	docker compose up -d postgres

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f postgres
