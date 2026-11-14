# Клонирование и запуск тестового Бобби с моками

Инструкция рассчитана на PowerShell на Windows-сервере. Стек `bobby-mocks` запускается отдельно от рабочего проекта. Jira, Google Calendar и GitLab обслуживаются локальными сервисами `mock-jira`, `mock-calendar` и `mock-gitlab`. Команда запуска сохраняет сервис команды, права, формы, напоминания, чат и поиск.

## 1. Получить код

Текущая версия опубликована в `main`. Для точного снимка отдельной ветки используйте `codex/mock-jira-calendar-20261113` вместо `main`:

```powershell
git clone --branch main --single-branch git@github.com:hih0lp/HAHACHANGE.git D:\HackChange\workspace\avito-rag-mock-services
Set-Location D:\HackChange\workspace\avito-rag-mock-services
```

Если SSH-ключ GitHub не настроен, используйте `https://github.com/hih0lp/HAHACHANGE.git` в той же команде. Не запускайте `git clone` поверх существующего каталога. Для обновления уже созданного тестового клона:

```powershell
Set-Location D:\HackChange\workspace\avito-rag-mock-services
git fetch origin
git switch main
git pull --ff-only origin main
```

Для перехода существующего клона на отдельную ветку:

```powershell
git fetch origin codex/mock-jira-calendar-20261113
git switch --track origin/codex/mock-jira-calendar-20261113
```

Если локальная ветка уже существует, вместо последней команды выполните `git switch codex/mock-jira-calendar-20261113` и `git pull --ff-only`.

## 2. Подготовить окружение

Нужны Docker Desktop с Compose, доступ к GitHub, работающий локальный LLM и общие сервисы поиска. Текущий Compose использует существующую сеть `avito-rag_default`, контейнеры `avito-rag-qdrant-1` и `avito-rag-embeddings-1`, а также соседние каталоги `D:\HackChange\workspace\knowledge` и `D:\HackChange\workspace\playbook`. Если этих ресурсов нет, сначала разверните их или измените конфигурацию Compose под свою инфраструктуру.

Создайте **отдельный тестовый** `.env` из `.env.example`:

```powershell
Copy-Item .env.example .env
notepad .env
```

Заполните независимыми тестовыми значениями `POSTGRES_PASSWORD`, `SESSION_SECRET`, `TEAM_ACCESS_INTERN_PASSWORD`, `TEAM_ACCESS_INTERN2_PASSWORD`, `TEAM_ACCESS_APPROVER_PASSWORD` (пароли команды не короче 12 символов). Укажите доступные из контейнера `LLM_URL` и `LLM_MODEL`, а также `JIRA_PROJECT_KEY` из `fixtures/mock/jira.json`, `CALENDAR_DEMO_SUBJECT` для тестового пользователя календаря и `TEAM_JIRA_ACCOUNTS_JSON`, связывающий `intern`, `intern2` и `approver` с отдельными Jira accountId. Готовое соответствие для тестового снимка приведено в [SDD-034](specs/034-test-jira-identities.md); `approver` соответствует роли «наставник» в чате. Для пингов перед дедлайном укажите `REMINDER_INTERN_MINUTES_BEFORE=10` и `REMINDER_MENTOR_MINUTES_BEFORE=5`; точное поведение описано в [SDD-035](specs/035-minute-deadline-pings.md). Файл `.env` не коммитьте; рабочий `.env` не копируйте.

Задачи и события находятся в `fixtures/mock/jira.json` и `fixtures/mock/calendar.json`. Исправления этих файлов применяются при следующем запросе без перезапуска сервисов. Ресурсы для формы доступа находятся в `fixtures/mock/resources.json`; после их изменения пересоздайте `access-rights`, чтобы обновить каталог. Формат и ограничения задач и событий описаны в [SDD-029](specs/029-editable-jira-calendar-mocks.md), а форму доступа — в [SDD-037](specs/037-http-form-idempotency.md).

## 3. Запустить тестовый стек

В PowerShell, из каталога клона:

```powershell
$env:BOBBY_LOCAL_PORT='18001'
$env:BOBBY_REMOTE_BIND='100.105.236.12'
$env:BOBBY_REMOTE_PORT='18001'
$env:BOBBY_TEAM_PORT='18082'
$env:BOBBY_RIGHTS_PORT='18083'
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml config --quiet
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml up -d --build --remove-orphans
```

`--remove-orphans` убирает старый контейнер GitLab только из проекта `bobby-mocks`. Рабочий Compose-проект не затрагивается. `100.105.236.12` — адрес текущего сервера в Tailscale; на другом сервере укажите его собственный Tailscale IP через `BOBBY_REMOTE_BIND`. Для доступа только с сервера можно указать свободный локальный адрес.

## 4. Проверить и обновлять

```powershell
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml ps
(Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18001/).StatusCode
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml logs --tail 50 api worker mock-jira mock-calendar mock-gitlab
```

Страница тестового чата: `http://100.105.236.12:18001/` с компьютера в той же сети Tailscale или `http://127.0.0.1:18001/` на сервере. Ожидается HTTP `200`; в списке сервисов должны быть `api`, `worker`, `mock-jira`, `mock-calendar`, `team-access` и `access-rights`. Контейнера `gitlab-integration` в проекте `bobby-mocks` быть не должно. API и worker получают адреса моков через Compose. В `compose.mock.yml` адрес GitLab MCP задан как `http://mock-gitlab:8080/mcp`.

После обновления кода снова выполните `git pull --ff-only` и команду `up -d --build --remove-orphans` с теми же переменными портов. Для остановки только тестового стека:

```powershell
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml down
```

Не добавляйте `-v`, если хотите сохранить тестовые чаты, учётные записи и ключи.
