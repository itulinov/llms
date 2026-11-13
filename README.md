# llms

Система управления обучением (LMS), созданная с использованием Go, React,
PostgreSQL и Docker.

Проект находится в стадии активной разработки.

## Запуск на сервере

1. Установите Git, Docker Engine и плагин Docker Compose. Убедитесь, что
   `docker compose version` работает и у пользователя есть доступ к Docker.

2. Склонируйте репозиторий и перейдите в его корень:

   ```bash
   git clone <URL_репозитория> lms
   cd lms
   ```

3. Создайте файл `.env` с паролем PostgreSQL:

   ```bash
   umask 077
   printf 'DB_PASSWORD=%s\n' "$(openssl rand -hex 32)" > .env
   ```

   Для генерации пароля нужен OpenSSL. Выполняйте этот шаг только при первом
   запуске: изменение `.env` не меняет пароль в уже созданной БД.

4. Соберите и запустите сервисы, затем примените миграции:

   ```bash
   docker compose up -d --build --wait
   ./scripts/migrate.sh
   ```

5. Проверьте состояние:

   ```bash
   docker compose ps
   curl http://localhost:8083/health
   ```

   Ожидаемый ответ: `{"database":"connected","status":"ok"}`.
   Портал доступен по `http://<IP_сервера>:8081`, админка —
   по `http://<IP_сервера>:8082`. Разрешите доступ к нужным портам в сетевых
   правилах сервера. API опубликован на порту `8083`.

Логи: `docker compose logs --tail=100`. Остановка: `docker compose down`.
Данные PostgreSQL сохраняются в томе `pgdata`; не используйте `down -v`,
если их нужно сохранить. Миграции автоматически при старте не запускаются.

## License

This project is source-available under the Apache
License, Version 2.0, subject to the Commons Clause
License Condition v1.0.

Internal use, including use within commercial
organizations, is permitted.

Selling the LMS itself or offering a paid service
whose value derives substantially from its
functionality is not permitted without separate
authorization from the copyright holder.

See [LICENSE](LICENSE) for the full legal terms.

## Серверная авторизация

Перед пересборкой API добавьте в локальный `.env` (не коммитить):

```dotenv
AUTH_COOKIE_SECURE=false
AUTH_ALLOWED_ORIGINS=http://localhost:8081,http://localhost:8082
AUTH_SESSION_TTL=12h
AUTH_TRUSTED_PROXIES=
```

Origins обязательны и включают схему и порт. Для доступа по IP сервера замените
localhost; для Vite добавьте его origin. Production требует HTTPS и
`AUTH_COOKIE_SECURE=true`. Cookie общая для портов одного хоста, но не для
разных хостов (например localhost и IP VirtualBox).

Внешние маршруты: `POST /api/auth/login` (JSON login/password),
`GET /api/auth/me`, `POST /api/auth/logout` (можно без тела),
`GET /api/admin/health` (роль admin). Изменяющие запросы требуют разрешённый
Origin, запросы с телом — application/json. Публичный `/health` сохранён.
Прокси Admin и Portal удаляют `/api/`; формы входа используют общий Go API.
Первого администратора создавайте служебной командой ниже, без ручных INSERT.

По умолчанию forwarded-заголовкам доверия нет. Для корректного IP-limiter за
Nginx задайте AUTH_TRUSTED_PROXIES списком точных доверенных CIDR через запятую.
Не доверяйте всей сети без необходимости: клиенты из доверенной сети смогут
влиять на определение адреса. Разбирается X-Forwarded-For справа налево.
Limiter хранится в памяти одного API и сбрасывается при перезапуске;
сессии хранятся в PostgreSQL и сохраняются после перезапуска.

```bash
cd api
go test -race ./...
go build ./...
# URL служебной тестовой PostgreSQL с правом CREATE DATABASE:
AUTH_TEST_DATABASE_URL='postgres://user:password@host/postgres' go test -race -run TestPostgresSessions -v
```

Интеграционный тест создаёт отдельную БД со случайным именем, затем удаляет её;
при отсутствии AUTH_TEST_DATABASE_URL он явно пропускается.

## Служебные команды авторизации

После пересборки и запуска API:

```bash
docker compose exec api /app/server create-admin --login admin
```

Введите пароль (12–128 Unicode-символов) дважды; ввод скрыт. Не используйте
`exec -T`: требуется терминал. Пароль нельзя передать аргументом или через pipe.
Команда создаёт активного администратора без кадровой карточки и отказывает,
если уже существует любой администратор, включая заблокированного.
Конкурирующие процессы защищены транзакционной advisory-блокировкой.

```bash
docker compose exec -T api /app/server cleanup-sessions
```

Очистка удаляет только просроченные сессии, печатает количество удалённых записей
и допускает повторные запуски. Её можно запускать периодически вручную или из
планировщика; действующие сессии не затрагиваются. Обе команды используют PG*
из окружения контейнера, не требуют AUTH_* и не запускают HTTP-сервер.

## Вход в Admin и Portal

Оба интерфейса проверяют `/api/auth/me`, поддерживают вход и выход.
Admin допускает только роль admin; Portal — admin и employee.
Cookie общая для портов одного хоста: `http://IP:8081` и `http://IP:8082`.
`localhost` и IP — разные хосты, общей сессии между ними нет.
После выхода другая вкладка обновляет состояние при возвращении в фокус.
Сеть и HTTP 503 показываются как временная недоступность, а не завершение сессии.

```bash
docker compose up -d --build api admin portal
# Если первый администратор ещё не создан:
docker compose exec api /app/server create-admin --login admin
```

Для Vite: добавьте `http://localhost:5173` (или фактический адрес браузера)
в AUTH_ALLOWED_ORIGINS, пересоздайте API, затем запустите `npm run dev` из admin.
Vite проксирует /api/ в localhost:8083; Portal локально обслуживается через Compose.

Локальные проверки:

```bash
node --test tests/auth.test.mjs
cd admin
npm install --package-lock=false
npm run build
npx playwright install firefox
cd ..
node tests/auth-browser.mjs
```

Playwright — только dev-зависимость; браузерные тесты подменяют API и не работают
с рабочими accounts. После git pull на сервере выполните Docker-сборку,
отложенные PostgreSQL/CLI-тесты в изолированной БД и проверку в Firefox:
вход администратора → обновление → Portal на том же хосте → выход →
возврат в Admin. Проверьте также запрещённый доступ сотрудника к Admin.
