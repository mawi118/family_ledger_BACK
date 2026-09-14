# family_ledger_BACK

## Требования

- Go 1.26+
- Docker / Docker Compose
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI

## Запуск


1. Поднять Postgres (база `family_ledger_full` создаётся автоматически при первом запуске контейнера):

```bash
docker compose up -d
```

2. Создать конфиг:

```bash
cp config/config.example.yaml config/config.yaml
```

В `config/config.yaml` задать свой `jwt.secret` (например: `openssl rand -hex 32`).

3. Накатить миграции:

```bash
migrate -database "postgres://family_ledger:family_ledger@localhost:5433/family_ledger_full?sslmode=disable" \
  -path migrations/prod up
```

4. Запустить сервер:

```bash
go run ./cmd/family_ledger
```

Сервер слушает gRPC на `localhost:5050`.

## Тестовые запросы

См. `requests.http`.

## Сборка / проверка

```bash
go build ./...
go vet ./...
```