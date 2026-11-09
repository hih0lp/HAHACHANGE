# AI-buddy: база знаний (Go)

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

Кнопка «Спросить Qwen» создаёт фоновое задание через `POST /v1/jobs`. Ограниченная очередь и два воркера по умолчанию выполняют запросы параллельно; интерфейс получает статусы и ответ через WebSocket. При обрыве соединения он проверяет статус по HTTP. Старый синхронный `POST /v1/ask` оставлен для совместимости. Кнопка «Только поиск» показывает результаты векторного поиска без генерации. Чек-лист перечисляет задачи, но не задаёт внутренний порядок оформления доступа; такой порядок в прототипе нужно помечать как тестовую имитацию.

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

### Тестовая Jira-заявка

На главной странице есть форма заявки на доступ. Пользователь вводит ресурс и причину, отмечает подтверждение; сервис создаёт тестовую задачу и показывает ключ, статус и ссылку. Результат хранится в Docker volume `jira-data` и доступен через `GET /v1/access-requests` и `GET /v1/access-requests/{id}`. Повторный `POST` с тем же заголовком `Idempotency-Key` возвращает сохранённую задачу.

По умолчанию `JIRA_MODE=mock`: задача сохраняется локально с ключом `SCRUM-MOCK-N`. Для тестового проекта `SCRUM` на `gapshinmihael.atlassian.net` задайте в локальном `.env` `JIRA_MODE=live`, `JIRA_EMAIL`, `JIRA_API_TOKEN` и `JIRA_ISSUE_TYPE=Задача`. API-токен создаётся в [профиле Atlassian](https://id.atlassian.com/manage-profile/security/api-tokens). `.env` не коммитится. Вход через Google в браузере не авторизует Go-сервис; ему нужен отдельный API-токен. Демо роль задаётся в запросе (`role: "intern"`) и не является защитой для рабочего окружения. Операции тестового проекта доступны каждому, кто имеет доступ к этому API.

Пример без фронтенда:

```bash
curl -X POST http://localhost:8000/v1/access-requests \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-request-001' \
  -d '{"resource":"Учебное пространство команды","reason":"Для онбординга стажёра","confirmed":true,"role":"intern"}'
```

Режим и схема описаны в `specs/003-jira-access-request.md`.

API и Qdrant опубликованы только на `127.0.0.1`. Для доступа из другой машины поменяйте порт в `docker-compose.yml` осознанно и задайте длинный `API_KEY` в `.env`. Тогда все вызовы, кроме `/health`, должны передавать заголовок:

```text
X-API-Key: <ваш API_KEY>
```

Векторная БД и модель эмбеддингов хранятся в именованных Docker volumes и переживают `docker compose down`. LM Studio и Qwen3 8B работают на Windows-хосте. Для полного сброса Docker-данных:

```bash
docker compose down -v
```

