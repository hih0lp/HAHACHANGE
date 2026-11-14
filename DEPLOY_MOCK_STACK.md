# Клонирование и запуск тестового Бобби с моками

Инструкция рассчитана на PowerShell на Windows-сервере. Стек `bobby-mocks` запускается отдельно от рабочего проекта. Jira и Google Calendar обслуживаются локальными сервисами `mock-jira` и `mock-calendar`; GitLab отключён. Команда запуска сохраняет сервис команды, права, формы, напоминания, чат и поиск.

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

Заполните независимыми тестовыми значениями `POSTGRES_PASSWORD`, `SESSION_SECRET`, `TEAM_ACCESS_INTERN_PASSWORD`, `TEAM_ACCESS_INTERN2_PASSWORD`, `TEAM_ACCESS_APPROVER_PASSWORD` (пароли команды не короче 12 символов). Укажите доступные из контейнера `LLM_URL` и `LLM_MODEL`, а также `JIRA_PROJECT_KEY` из `fixtures/mock/jira.json`, `CALENDAR_DEMO_SUBJECT` для тестового пользователя календаря и `TEAM_JIRA_ACCOUNTS_JSON`, связывающий пользователя `intern` с `assignee_account_id` из Jira JSON. Пример формы последнего значения: `{"intern":"<assignee_account_id из jira.json>"}`. Файл `.env` не коммитьте; рабочий `.env` не копируйте.

Задачи и события находятся в `fixtures/mock/jira.json` и `fixtures/mock/calendar.json`. Исправления этих файлов применяются при следующем запросе без перезапуска сервисов. Формат и ограничения полей описаны в [SDD-029](specs/029-editable-jira-calendar-mocks.md).

## 3. Запустить тестовый стек

В PowerShell, из каталога клона:

```powershell
$env:BOBBY_LOCAL_PORT='18001'
$env:BOBBY_REMOTE_BIND='127.0.0.2'
$env:BOBBY_REMOTE_PORT='18001'
$env:BOBBY_TEAM_PORT='18082'
$env:BOBBY_RIGHTS_PORT='18083'
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml config --quiet
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml up -d --build --remove-orphans
```

`--remove-orphans` убирает старый контейнер GitLab только из проекта `bobby-mocks`. Рабочий Compose-проект не затрагивается. Проброс `127.0.0.2` рассчитан на текущий сервер; если этот адрес недоступен, назначьте другой свободный локальный адрес и порт через `BOBBY_REMOTE_BIND` и `BOBBY_REMOTE_PORT`.

## 4. Проверить и обновлять

```powershell
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml ps
(Invoke-WebRequest -UseBasicParsing http://127.0.0.1:18001/).StatusCode
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml logs --tail 50 api worker mock-jira mock-calendar
```

Страница тестового чата: `http://127.0.0.1:18001/` на сервере. Ожидается HTTP `200`; в списке сервисов должны быть `api`, `worker`, `mock-jira`, `mock-calendar`, `team-access` и `access-rights`. Контейнера `gitlab-integration` в проекте `bobby-mocks` быть не должно. API и worker получают адреса моков и пустой `GITLAB_MCP_URL` непосредственно из `compose.distributed.yml`.

После обновления кода снова выполните `git pull --ff-only` и команду `up -d --build --remove-orphans` с теми же переменными портов. Для остановки только тестового стека:

```powershell
docker compose -p bobby-mocks -f compose.distributed.yml -f compose.mock.yml down
```

Не добавляйте `-v`, если хотите сохранить тестовые чаты, учётные записи и ключи.
