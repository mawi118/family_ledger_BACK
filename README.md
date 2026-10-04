# family_ledger_BACK

## Быстрый запуск (Docker, без Go)

Единственное требование - установленный Docker / Docker Compose. Все команды выполняются из корня репозитория.

1. Создать конфиг:

```bash
cp config/config.docker.example.yaml config/config.yaml
```

В `config/config.yaml` задать свой `jwt.secret` (например: `openssl rand -hex 32`). Значения `access_ttl_minutes` (15) и `refresh_ttl_days` (30) можно оставить по умолчанию.

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

## Аутентификация

Схема токенов — access + refresh, без единого stateful-сеанса:

- **Access-токен** — короткоживущий (`jwt.access_ttl_minutes`, по умолчанию 15 минут) подписанный JWT. Проверяется только по подписи и `exp`, без обращения к базе — поэтому он остаётся действителен до истечения TTL даже после `Logout`.
- **Refresh-токен** — непрозрачный случайный токен (не JWT) с TTL `jwt.refresh_ttl_days` (по умолчанию 30 дней). В базе хранится только его SHA-256 хеш (таблица `refresh_tokens`), сам токен нигде не сохраняется.
- **Ротация**: каждый вызов `Refresh` ревокает старый refresh-токен и выдаёт новую пару access+refresh. Повторное использование уже отревоканного refresh-токена отклоняется — это и есть механизм обнаружения кражи токена.
- **Logout** — реальная ревокация: `revoked_at` выставляется в базе по хешу refresh-токена. Access-токен при этом не инвалидируется (см. выше — он просто доживает свой TTL).
- **Me** — отдаёт `{id, email, firstName}` по access-токену. Данные всегда читаются свежими из таблицы `users` (не из claims JWT), чтобы не отдавать устаревшую информацию.

Сервис `Auth` (см. `proto/auth.proto`): `Register`, `Login`, `EmailExists`, `Refresh`, `Logout`, `Me`.

## Тестовые запросы

Базовые ручные запросы (`EmailExists`/`Register`/`Login`) — см. `requests.http`.

Полный жизненный цикл (включая `Refresh`/`Logout`/`Me`, ротацию и ревокацию) покрыт интеграционными тестами — см. следующий раздел.

## Тесты

Интеграционные тесты (`internal/auth/server_test.go`) поднимают gRPC-сервер in-process (`bufconn`) и требуют реальный Postgres:

```bash
export TEST_DATABASE_URL="postgres://family_ledger:family_ledger@localhost:5433/family_ledger_full?sslmode=disable"
go test ./... -v -p 1
```

Перед первым запуском накатите миграции на эту базу (см. шаг 3 в "Локальная разработка").

## Сборка / проверка

```bash
go build ./...
go vet ./...
```

## CI/CD

- **Тесты** (`.github/workflows/test.yml`) — запускаются на каждый PR и push в `main` (поднимают Postgres как service-контейнер, накатывают миграции, гоняют `go test ./...`).
- **Деплой** (`.github/workflows/deploy.yml`) — на каждый push в `main` собирает образ, пушит его в `ghcr.io/mawi118/family_ledger_back` (тегами `latest` и `<sha>`) и деплоит на VPS по SSH (ключ в GitHub Actions ограничен так, что может выполнить только фиксированный `deploy.sh` на сервере — ничего больше).