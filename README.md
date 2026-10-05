# family_ledger_BACK

gRPC-бэкенд семейного бюджета (Go, PostgreSQL). Сервисы: `Auth`, `Groups`, `Health`. Отклонения от требований и их статус записаны в [REQUIREMENTS_DEVIATIONS.md](REQUIREMENTS_DEVIATIONS.md).

## Быстрый запуск (Docker, без Go)

Единственное требование - установленный Docker / Docker Compose. Все команды выполняются из корня репозитория.

1. Создать конфиг:

```bash
cp config/config.docker.example.yaml config/config.yaml
```

В `config/config.yaml` задать свой `jwt.secret` (например: `openssl rand -hex 32`). Значения `access_ttl_minutes` (15) и `refresh_ttl_days` (7) можно оставить по умолчанию.

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

### Изменение `.proto`

После правки файла в `proto/` нужно перегенерировать Go-код (одна команда на файл, из корня репозитория). Пример для групп:

```bash
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/group.proto
```

Копию изменённого `.proto` и сгенерированные TypeScript-типы нужно обновить и в BFF (репозиторий `family_ledger_FRONT`, папка `family-ledger-bff`).

## Аутентификация

Схема токенов: access + refresh, одна живая сессия на пользователя.

- **Access-токен** - короткоживущий (`jwt.access_ttl_minutes`, по умолчанию 15 минут) JWT (только HS256) с claims `sub` (id пользователя), `jti`, `iat`, `exp`. `jti` равен id строки в `refresh_tokens`, к которой токен привязан. Токен принимается, только пока подпись и срок в порядке и эта строка не отозвана и не истекла (один запрос по первичному ключу). Поэтому `Logout`, новый `Login` и `Refresh` мгновенно обрывают прежний access.
- **Refresh-токен** - непрозрачный случайный токен (не JWT) с TTL `jwt.refresh_ttl_days` (по умолчанию 7 дней). В базе хранится только его SHA-256 хеш (таблица `refresh_tokens`).
- **Ротация**: каждый `Refresh` атомарно отзывает старый refresh-токен и выдаёт новую пару access+refresh. Из двух параллельных запросов с одним токеном проходит ровно один.
- **Одна сессия**: `Login` и `Logout` отзывают все живые refresh-токены пользователя. Вход на втором устройстве разлогинивает первое (требование дока 3).
- **Login**: на любую неудачу один и тот же ответ «Неверное имя пользователя или пароль», время ответа для несуществующей почты не отличается.
- **Нормализация ввода** (до валидации): почта обрезается и приводится к нижнему регистру, имя обрезается и получает заглавную первую букву.
- **Me** - отдаёт `{id, email, firstName}`; данные читаются из таблицы `users`, не из claims.
- Роли в токен не кладутся: роль в группе хранится в `group_members.role` (`owner` / `member`).

Сервис `Auth` (см. `proto/auth.proto`): `Register`, `Login`, `EmailExists`, `Refresh`, `Logout`, `Me`.

### Авторизация остальных методов

Все методы, кроме `Auth.*` и `Health.HealthCheck`, требуют access-токен в gRPC-метаданных:

```
authorization: Bearer <access_token>
```

Это делает `internal/interceptor/auth.go` (запрет по умолчанию: новый метод закрыт, пока его явно не добавят в список публичных). Идентификатор пользователя берётся только из проверенного токена, не из тела запроса. Порядок перехватчиков: сначала авторизация, потом валидация (`Normalize` -> `Validate`).

## Группы

Сервис `Groups` (см. `proto/group.proto`): `CreateGroup`, `ListMyGroups`, `GetGroup`, `RenameGroup`, `ListMembers`, `GetMyIncome`, `SetMyIncome`, `CreateInvite`, `AcceptInvite`.

- Роли: создатель (`owner`, ровно один на группу) и участник (`member`). Переименовать группу и создать код приглашения может только создатель (остальные получают `PermissionDenied`).
- Группа и участие: не участник и несуществующая группа неотличимы (`NotFound`), чужие id не раскрываются. В группе не больше 10 участников.
- `CreateGroup`: `name` необязателен, пустое имя означает «Группа {X+1}», где X - число групп пользователя.
- Участники отдаются в порядке: сам запрашивающий, создатель, остальные по дате вступления. Доход хранится в копейках (RUB), `0 <= income <= 10^12`; свой доход меняет только сам участник.
- Приглашения: код из 4 цифр (`crypto/rand`), живёт 5 минут, одноразовый. Пока код жив, повторный `CreateInvite` возвращает тот же. Использованный код считается истёкшим: создатель сразу получает новый. Код уникален среди всех живых приглашений (таблица `group_invites`).
- Защита от подбора кода: не больше 5 неверных вводов на пользователя за 5 минут (`ResourceExhausted`). Счётчик хранится в памяти процесса и сбрасывается при перезапуске.
- Если пользователь уже в группе или группа заполнена, код при отказе не сгорает (вся операция в одной транзакции под блокировкой строки группы).

Схема БД описана в миграциях `migrations/prod/` (актуально на `000004_groups`).

## Тестовые запросы

Базовые ручные запросы (`EmailExists`/`Register`/`Login`) - см. `requests.http` (файл устарел, примеры могут не работать с текущей валидацией).

Полные сценарии (сессии, ротация, ревокация, группы, приглашения, гонки) покрыты интеграционными тестами - см. следующий раздел.

## Тесты

Интеграционные тесты (`internal/auth`, `internal/group`) поднимают gRPC-сервер in-process (`bufconn`) и требуют реальный Postgres. **Тесты стирают данные (`TRUNCATE`)**, поэтому им нужна отдельная тестовая БД, а не рабочая. Пакеты делят одну БД, поэтому запускаются строго последовательно (`-p 1`).

1. Создать тестовую БД (один раз) и накатить на неё миграции:

```bash
docker compose exec postgres psql -U family_ledger -d postgres -c "CREATE DATABASE family_ledger_test;"
migrate -database "postgres://family_ledger:family_ledger@localhost:5433/family_ledger_test?sslmode=disable" -path migrations/prod up
```

2. Запустить тесты (bash):

```bash
export TEST_DATABASE_URL="postgres://family_ledger:family_ledger@localhost:5433/family_ledger_test?sslmode=disable"
go test ./... -v -p 1
```

В PowerShell переменная задаётся так: `$env:TEST_DATABASE_URL="postgres://family_ledger:family_ledger@localhost:5433/family_ledger_test?sslmode=disable"`.

Без `TEST_DATABASE_URL` интеграционные тесты пропускаются (`SKIP`), выполняются только юнит-тесты валидации.

## Сборка / проверка

```bash
go build ./...
go vet ./...
```

## CI/CD

- **Тесты** (`.github/workflows/test.yml`) - запускаются на каждый PR и push в `main` (поднимают Postgres как service-контейнер, накатывают миграции, гоняют `go test ./... -v -p 1`).
- **Деплой** (`.github/workflows/deploy.yml`) - на каждый push в `main` собирает образ, пушит его в `ghcr.io/mawi118/family_ledger_back` (тегами `latest` и `<sha>`) и деплоит на VPS по SSH (ключ в GitHub Actions ограничен так, что может выполнить только фиксированный `deploy.sh` на сервере). Миграции применяет сервис `migrate` из `docker-compose.yml` перед стартом бэкенда. Деплой не ждёт результата тестов (гейт тестов есть только на PR).