# Спецификация: Avito Playbook RAG

## Назначение

Сервис даёт ИИ-агенту семантический поиск по Markdown-документам Playbook. Он возвращает короткие фрагменты вместе с источником, а агент добавляет их в контекст модели и формирует ответ с цитированием файла.

## Состав модулей

| Модуль | Назначение | Хранение состояния |
| --- | --- | --- |
| `playbook/` | Исходные Markdown-документы для индексации | Git-репозиторий / файловая система |
| `api` | Go HTTP API: разбиение документов на фрагменты, индексация, поиск, авторизация | Stateless |
| `embeddings` | Локальный Hugging Face Text Embeddings Inference c `intfloat/multilingual-e5-small` | `model-cache` Docker volume |
| `qdrant` | Векторная БД: векторы, текст фрагментов и метаданные источника | `qdrant-data` Docker volume |
| `pkg/knowledge` | Go-клиент, который подключается в код будущего агента | В коде агента |

Поток данных: `playbook/*.md` → Go API (chunking) → embeddings → Qdrant → `/v1/knowledge/search` → агент → LLM-ответ.

## Развёртывание на сервере

Рабочая папка: `C:\Users\ADMIN\Desktop\Hack&Change\avito-rag`.

```bat
cd /d "C:\Users\ADMIN\Desktop\Hack&Change\avito-rag"
copy .env.example .env
docker compose up -d
docker compose ps
```

Проверка готовности:

```bat
curl http://localhost:8000/health
```

Первый старт модели занимает несколько минут: веса сохраняются в `model-cache`, поэтому последующие перезапуски быстрее.

### Офлайн-загрузка образов

Если Docker Desktop не может получить публичные образы из-за ошибки credential helper, используйте заранее подготовленный tar-файл:

```bat
docker load -i .docker-images\avito-rag-server-images.tar
docker compose up -d
```

Это не меняет Docker-аккаунт: команда лишь загружает в локальный Docker Engine те же образы Qdrant, TEI и Go API.

## API-контракт

| Метод | URL | Назначение |
| --- | --- | --- |
| `GET` | `/health` | Готовность сервиса и количество проиндексированных фрагментов |
| `GET` | `/` | Простая веб-страница для ручного поиска и проверки результатов |
| `GET` | `/v1/knowledge/stats` | Число документов и фрагментов |
| `POST` | `/v1/knowledge/search` | Семантический поиск для агента |
| `POST` | `/v1/ask` | RAG-ответ через Qwen3 8B в LM Studio с источниками |
| `POST` | `/v1/knowledge/reindex` | Полная пересборка индекса после обновления документов |

Запрос поиска:

```json
{"query":"Как проходит ревью технического дизайна?","limit":3}
```

Ответ:

```json
{
  "hits": [
    {
      "score": 0.72,
      "path": "tech_design_review.md",
      "title": "Ревью технического дизайна",
      "chunk_index": 2,
      "text": "Текст релевантного фрагмента"
    }
  ]
}
```

## Подключение ИИ-агента

1. Агент принимает вопрос пользователя.
2. Вызывает `POST /v1/knowledge/search` с `limit` 3–5.
3. Собирает поля `text` в контекст LLM, сохраняя `path` и `chunk_index`.
4. Просит LLM отвечать только на основе найденного контекста и возвращать источники.

Для Go-агента используйте `pkg/knowledge`:

```go
client := knowledge.Client{BaseURL: "http://localhost:8000"}
hits, err := client.Search(ctx, question, 5)
if err != nil {
    return err
}
contextForLLM := knowledge.BuildContext(hits)
```

## Обновление знаний

```bat
git -C ..\playbook pull --ff-only
curl -X POST http://localhost:8000/v1/knowledge/reindex
```

`/reindex` очищает и строит индекс заново, поэтому удалённые документы не остаются в выдаче.

## Безопасность и эксплуатация

- По умолчанию API и Qdrant доступны только на `127.0.0.1`.
- Для публикации API задайте длинный `API_KEY` в `.env`; передавайте его в заголовке `X-API-Key`.
- Не публикуйте Qdrant-порт наружу без сетевой защиты.
- Состояние не теряется при `docker compose down`; полный сброс — `docker compose down -v`.
- Логи: `docker compose logs -f api embeddings qdrant`.

## Качество

Go API собирается только после тестов. Dockerfile проверяет суммарное покрытие `go test` не ниже 90%.
