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

## Сотрудники и учётные записи (этап 05)

В Admin раздел «Сотрудники» поддерживает поиск, фильтр статуса, создание и
редактирование основных кадровых полей. Карточка использует updated_at для
защиты от потери изменений: конфликт 409 требует загрузить актуальную версию.
Кадровый логин, data и должности редактор не меняет.

Доступ employee создаётся в карточке. Блокировка, увольнение и новый пароль
отзывают все сессии. Возврат в штат включает доступ даже после ручной блокировки;
интерфейс предупреждает об этом. Разблокировка уволенного отдельно запрещена.
Создание admin и смена роли через интерфейс не поддерживаются.

API: GET/POST /api/admin/collaborators, GET/PUT /api/admin/collaborators/{id},
POST .../{id}/account, PUT .../{id}/account/status, POST .../{id}/account/password.
Все endpoints требуют admin. Новый SQL не нужен.

Проверки: `go test -race ./...` из api (с AUTH_TEST_DATABASE_URL также проверяется
жизненный цикл сотрудника в отдельной БД); после сборки Admin:
`node tests/employees-browser.mjs`.

### Перед первым рабочим развёртыванием этапа 05

Создайте backup в исключённом из Git каталоге:

```bash
umask 077
mkdir -p backups
docker compose exec -T db pg_dump -U lms -d lms -Fc > backups/before-stage05.dump
# Проверить восстановлением в отдельную пустую БД:
docker compose exec -T db createdb -U lms lms_backup_verify_stage05
docker compose exec -T db pg_restore -U lms -d lms_backup_verify_stage05 --exit-on-error < backups/before-stage05.dump
docker compose exec -T db psql -U lms -d lms_backup_verify_stage05 -c 'SELECT count(*) FROM collaborators; SELECT count(*) FROM accounts;'
docker compose exec -T db dropdb -U lms lms_backup_verify_stage05
```

Если любое действие завершается ошибкой, остановите развёртывание и разберите
причину; не используйте рабочую lms как цель pg_restore. После проверки backup
выполните `docker compose up -d --build api admin`, затем проверьте реальный цикл
создания сотрудника, входа, увольнения и восстановления через Firefox.

## Оргструктура и должности (этап 06)

Admin → «Оргструктура»: организации, дерево подразделений, штатные единицы.
На широком экране используются три колонки, на мобильном — последовательные
экраны с возвратом. Центры затрат и типовые должности в редактор не включены.

Создайте организацию, подразделение и вакантную должность. В карточке должности
найдите работающего сотрудника и закрепите его. Кадровая дата необязательна;
пустая дата означает текущую дату PostgreSQL. Дата может предшествовать приёму:
дополнительные кадровые ограничения не вводились.

Карточка сотрудника содержит должности, выбор/снятие основной и историю.
PostgreSQL автоматически делает единственную должность основной и очищает
основную при откреплении. История хранит снимки названий и сортируется по
последовательности регистрации event_seq, отдельно от кадровой даты.
Закрытие подразделения или должности не выполняет каскадных кадровых операций;
увольнение также не открепляет должности. Для замены сотрудника на вакансии
сначала явно открепите прежнего.

API под /api/admin: orgs, subdivisions, positions (GET/POST и GET/PUT по UUID),
POST positions/{id}/assign и /unassign; GET collaborators/{id}/positions,
PUT collaborators/{id}/main-position, GET collaborators/{id}/position-history.
PUT и кадровые действия требуют точный updated_at. Конфликт 409 требует обновления.

```bash
# Из api, с доступным изолированным PostgreSQL:
AUTH_TEST_DATABASE_URL='postgres://user:password@host/postgres' go test -race -count=1 -v ./...
# Из корня после сборки Admin:
node tests/structure-browser.mjs
```

Новая миграция не нужна. Перед рабочей поставкой повторите процедуру backup и
проверки восстановления из раздела этапа 05, используя новое имя dump/проверочной БД.
Затем пересоберите api и admin и проверьте кадровый цикл в Firefox на сервере.
Локальные тесты PostgreSQL не заменяют серверную приёмку Docker/Nginx.
