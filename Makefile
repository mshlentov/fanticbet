.PHONY: run build migrate-up migrate-down migrate-new docker-up docker-down

# ─── Запуск ────────────────────────────────────────────

run:
	go run cmd/server/main.go

build:
	go build -o bin/server cmd/server/main.go

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
