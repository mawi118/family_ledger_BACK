# family_ledger_BACK

## Требования

- Go 1.26+
- Docker / Docker Compose
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI

## Запуск

1. Поднять Postgres:

   docker compose up -d

2. Создать конфиг:

   cp config/config.example.yaml config/config.yaml

   В `config/config.yaml` задать свой `jwt.secret` (например: `openssl rand -hex 32`).

3. Создать базу :

   psql -h localhost -p 5433 -U family_ledger -d postgres -c "CREATE DATABASE family_ledger_full"

4. Накатить миграции:

   migrate -database "postgres://family_ledger:family_ledger@localhost:5433/family_ledger_full?sslmode=disable" \
   -path migrations/prod up

5. Запустить сервер:

   go run ./cmd/family_ledger

   Сервер слушает gRPC на `localhost:5050`.

## Тестовые запросы

См. `requests.http`.

## Сборка / проверка

go build ./...
go vet ./...