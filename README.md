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

На первом запуске образ модели и её веса могут скачиваться несколько минут. API готов, когда команда вернёт `"status":"ok"`:

```bash
curl http://localhost:8000/health
```

Поиск:

Для ручной проверки откройте `http://localhost:8000/` на сервере. Если работаете со своего Mac, создайте SSH-туннель `ssh -L 8000:127.0.0.1:8000 hackchange_collab@100.105.236.12` и откройте тот же адрес в браузере.

Кнопка «Спросить Qwen» вызывает `POST /v1/ask`: сервис находит пять фрагментов и передаёт их в Qwen3 8B через LM Studio на сервере (`LLM_URL`, `LLM_MODEL`). Кнопка «Только поиск» показывает результаты векторного поиска без генерации. Чек-лист перечисляет задачи, но не задаёт внутренний порядок оформления доступа; такой порядок в прототипе нужно помечать как тестовую имитацию.

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

API и Qdrant опубликованы только на `127.0.0.1`. Для доступа из другой машины поменяйте порт в `docker-compose.yml` осознанно и задайте длинный `API_KEY` в `.env`. Тогда все вызовы, кроме `/health`, должны передавать заголовок:

```text
X-API-Key: <ваш API_KEY>
```

Векторная БД и модель эмбеддингов хранятся в именованных Docker volumes и переживают `docker compose down`. LM Studio и Qwen3 8B работают на Windows-хосте. Для полного сброса Docker-данных:

```bash
docker compose down -v
```

