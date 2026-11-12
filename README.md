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
