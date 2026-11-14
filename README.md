# AI-buddy: база знаний (Go)

## Два Go-сервиса для команды и доступов

`cmd/server` — Бобби; `cmd/team-access` — каталог команды, проверка прав и заявки по [SDD-013](specs/013-team-access-contract.md). На этой машине распределённый стек доступен по `http://127.0.0.1:18000/`, сервис доступа — по `http://127.0.0.1:18080/health`. Вход для сценария: `http://127.0.0.1:18000/onboarding`. Пароли демо-пользователей `intern`, `intern2`, `approver` лежат только в локальном `.env`.

Запуск: сначала `docker compose up -d` для Qdrant и embeddings, затем `docker compose -p avito-rag-distributed -f compose.distributed.yml up -d --build`. Для проверки: `go test ./...` и `scripts/team-access-smoke.ps1`. Подробности интеграции и ограничения локального HTTP — в [SDD-012](specs/012-team-access-client-integration.md).

Go-сервис семантического поиска по Markdown-документам из `../playbook` и выдержке из открытого чек-листа в `../knowledge`. Он состоит из контейнеров и запускается на macOS, Linux и Windows с Docker Desktop:

- **Go API** — загрузка документов, разбиение на фрагменты и API для агента;
- **Qdrant** — персистентная векторная БД;
- **Hugging Face Text Embeddings Inference** — локальные русско- и англоязычные эмбеддинги `intfloat/multilingual-e5-small`.

Содержимое документов не отправляется во внешний LLM или API. При первом запуске TEI скачает публичную модель в локальный Docker volume.

На Mac с Apple Silicon сервис эмбеддингов запускается Docker Desktop в режиме `linux/amd64`, потому что официальный CPU-образ TEI пока выпускается только для этой архитектуры. Настройки Compose уже содержат нужную платформу; на x86 Linux контейнер работает нативно.

## Структура

```text
HackChange/
├── playbook/       # клон avito-tech/playbook
├── knowledge/      # выдержка из открытого чек-листа стажёра
└── avito-rag/      # этот проект
```

## Запуск

```bash
cd ~/HackChange/avito-rag
cp .env.example .env
docker compose up --build -d
docker compose logs -f api
```


Если Docker Desktop не выдерживает сборку Go-образа из-за нехватки места на системном диске, можно собрать Linux-бинарник установленным Go и использовать `Dockerfile.prebuilt`:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -o bin/knowledge-api-linux ./cmd/server
docker buildx build --load -f Dockerfile.prebuilt -t avito-rag-api:server .
docker compose up -d --no-build api
```

На первом запуске образ модели и её веса могут скачиваться несколько минут. API готов, когда команда вернёт `"status":"ok"`:

```bash
curl http://localhost:8000/health
```

Поиск:

Для ручной проверки откройте `http://localhost:8000/` на сервере. С Mac можно создать SSH-туннель `ssh -L 18000:127.0.0.1:8000 hackchange_collab@100.105.236.12` и открыть `http://localhost:18000/`.

Кнопка «Спросить Qwen» создаёт фоновое задание через `POST /v1/jobs`. Ограниченная очередь и два воркера по умолчанию выполняют запросы параллельно; интерфейс получает статусы и ответ через WebSocket. При обрыве соединения он проверяет статус по HTTP. На странице показывается только текст ответа модели; ссылки на источники внутри ответа кликабельны. Найденные фрагменты и отдельные карточки документов не выводятся. `POST /v1/knowledge/search` остаётся доступным агенту, а синхронный `POST /v1/ask` — для совместимости. Чек-лист перечисляет задачи, но не задаёт внутренний порядок оформления доступа; такой порядок в прототипе нужно помечать как тестовую имитацию.

Стартовая страница — простой чат с Бобби, учебным ИИ-помощником стажёра. Он отвечает и на обычные вопросы без обязательного документа или ссылки. Для внутренних процессов и задач Jira нужны подтверждающие источники; недостающие сведения он не додумывает. Форма тестовой Jira-заявки отключена: в доступных источниках не найден подтверждённый порядок оформления доступа. В распределённом режиме вопросы и ответы хранятся в PostgreSQL и восстанавливаются при открытии страницы через `GET /v1/chat/history` для текущей демо-сессии. Для назначенных стажёру 1 незавершённых задач Jira чат сохраняет напоминания за `REMINDER_INTERN_MINUTES_BEFORE` минут ему и за `REMINDER_MENTOR_MINUTES_BEFORE` минут наставнику. Jira `due_date` без времени считается концом указанного дня в `REMINDER_TIMEZONE`. Старый маршрут `/v1/reminders` использует отдельную настройку `REMINDER_HOURS_BEFORE` и не дублирует карточки чата.

`LLM_URL`, `LLM_MODEL`, `JOB_WORKERS`, `JOB_QUEUE_SIZE` и `JOB_TIMEOUT_SECONDS` настраиваются через `.env`; Compose не переопределяет их фиксированными значениями.

Контракт асинхронного запроса:

```text
POST /v1/jobs                 {"query":"..."} → 202 {"id":"...","status":"queued"}
GET  /v1/jobs/{id}           → текущий статус и результат
WS   /v1/jobs/{id}/ws        → снимок и события queued/running/completed/failed
```

`JOB_WORKERS`, `JOB_QUEUE_SIZE` и `JOB_TIMEOUT_SECONDS` задаются в `.env`. При заполненной очереди сервер отвечает `429`. Задания хранятся в памяти и теряются при перезапуске API. Если задан `API_KEY`, первым WebSocket-сообщением отправьте `{"api_key":"..."}`; ключ не помещается в URL. Поведение описано в `specs/002-async-requests-websocket.md`.

```bash
curl -s http://localhost:8000/v1/knowledge/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"Как проходит ревью технического дизайна?", "limit": 3}'
```

Ответ возвращает текст, релевантность и источник каждого фрагмента:

```json
{
  "hits": [{
    "score": 0.72,
    "path": "tech_design_review.md",
    "title": "Ревью технического дизайна",
    "chunk_index": 2,
    "text": "..."
  }]
}
```

## Заготовка для вашего агента

Пакет `pkg/knowledge` — готовый клиент для Go-агента. Он вызывает RAG API и формирует контекст с источниками:

```go
client := knowledge.Client{BaseURL: "http://localhost:8000"}
hits, err := client.Search(ctx, userQuestion, 5)
if err != nil { /* обработать ошибку */ }

ragContext := knowledge.BuildContext(hits)
// Добавьте ragContext в prompt/messages вашего LLM-агента.
```

Рекомендуемый порядок в агенте: запрос пользователя → `Search` → добавить `BuildContext(hits)` в системный/контекстный промпт → ответить с указанием `path` источника. Это отделяет RAG от конкретного SDK LLM, поэтому можно подключить OpenAI, YandexGPT, GigaChat или локальную модель.

## Обновление базы

После обновления Playbook или файла в `../knowledge`:

```bash
git -C ../playbook pull --ff-only
curl -X POST http://localhost:8000/v1/knowledge/reindex
```

`/reindex` полностью пересобирает коллекцию, поэтому удалённые документы не останутся в выдаче.

## Доступ и данные

### Команда и доступы

Прежняя форма Jira-заявки и маршрут создания отключены. Отдельный Go-сервис хранит профиль команды, назначение наставника, права и заявки в PostgreSQL. В распределённом режиме Бобби получает проверенные данные этого сервиса через `TEAM_ACCESS_URL`; чат использует их при наличии JWT пользователя. Сценарий первой задачи сохраняет своё состояние в PostgreSQL. Если данных нет, помощник сообщает об отсутствии подтверждения. Распределённый стек подключает только редактируемые Jira и Calendar MCP моки; `JIRA_MODE` допускает `off` и `mock`. Формат данных и запуск описаны в `specs/030-mock-only-integrations.md`.

### Jira через MCP в ответах агента

Инструменты чтения `jira_get_issue`, `jira_list_issues` и `jira_list_assigned_issues` предоставляет только сервис `mock-jira` по внутреннему адресу `http://mock-jira:8080/mcp`. Бобби выбирает их через модель и читает редактируемый `fixtures/mock/jira.json`. Создание заявки через Jira отключено; формы доступа обслуживает сервис прав. Контракт текущего запуска описан в `specs/030-mock-only-integrations.md`.

API и Qdrant опубликованы только на `127.0.0.1`. Для доступа из другой машины поменяйте порт в `docker-compose.yml` осознанно и задайте длинный `API_KEY` в `.env`. Тогда все вызовы, кроме `/health`, должны передавать заголовок:

```text
X-API-Key: <ваш API_KEY>
```

Векторная БД и модель эмбеддингов хранятся в именованных Docker volumes и переживают `docker compose down`. LM Studio и Qwen3 8B работают на Windows-хосте. Для полного сброса Docker-данных:

```bash
docker compose down -v
```


### Демо корпоративных групп

Отдельный сервис хранит тестовые переписки в PostgreSQL. В HTML-форме можно добавить сообщение, а MCP позволяет искать по группам текущего пользователя. [Запуск и контракт MCP](specs/033-group-chat-mcp.md).

