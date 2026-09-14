# family_ledger_BACK

## Быстрый запуск (Docker, без Go)

Единственное требование - установленный Docker / Docker Compose. Все команды выполняются из корня репозитория.

1. Создать конфиг:

```bash
cp config/config.docker.example.yaml config/config.yaml
```

В `config/config.yaml` задать свой `jwt.secret` (например: `openssl rand -hex 32`).

2. Собрать и поднять всё сразу (Postgres, миграции, сервер):

```bash
docker compose up -d --build
```

3. Проверить, что сервер поднялся:

```bash
docker compose logs -f backend
```

Сервер слушает gRPC на `localhost:5050`.

## Локальная разработка (с Go)

Требования:

- Go 1.26+
- Docker / Docker Compose
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI

Все команды выполняются из корня репозитория (сервер при старте ищет `config/config.yaml` по относительному пути).

1. Поднять только Postgres (без сборки бэкенда и без авто-миграций):

```bash
docker compose up -d postgres
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