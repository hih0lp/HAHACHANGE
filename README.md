# Бобби — помощник стажёра

## Локальный запуск

Нужны Docker Desktop с Compose и около 12 ГБ памяти, выделенной Docker Desktop.
Локальный стек использует Qwen3 8B на CPU. Для простоты настройки Ollama
обрабатывает один запрос за раз; остальные ждут очереди. При необходимости
число одновременных запросов можно изменить в `OLLAMA_NUM_PARALLEL` файла
`compose.local.yml`. При текущей настройке пробный запуск модели прошёл с
12 ГБ памяти Docker. При первом запуске
скачивается около 5,2 ГБ модели; загрузка и ответы на CPU могут быть долгими.
Локальный стенд также тестировали на компьютере с NVIDIA GeForce RTX 5070 Ti
и 32 ГБ оперативной памяти.

Из корня репозитория выполните:

```bash
docker compose -f compose.local.yml up --build --wait --wait-timeout 1200
```

Откройте [чат Бобби](http://127.0.0.1:18001/) или
[демомессенджер](http://127.0.0.1:28085/). Локальный Compose собирает фронтенд
и Go-сервисы, запускает PostgreSQL, Redis, Kafka, Qdrant, сервис эмбеддингов,
Ollama и отдельные MCP-сервисы для Jira, календаря, GitLab, группового чата,
команды и прав доступа. Порты опубликованы только на `127.0.0.1`.

Учебные документы находятся в `fixtures/knowledge`; публичный Playbook
монтируется из соседнего каталога `../playbook`. Если Playbook нужен для поиска,
разместите его в этом каталоге перед запуском. Файлы
`fixtures/mock/jira.json`, `fixtures/mock/calendar.json` и
`fixtures/mock/company-calendar.json` задают начальные данные моков. При первом
запуске они загружаются в PostgreSQL; последующие изменения через редактор
сохраняются в БД и не затираются при перезапуске. Модели и данные сохраняются
в томах Compose-проекта `bobby-local`.

Модель можно переопределить переменной `BOBBY_MODEL`, порты — переменными
`BOBBY_LOCAL_PORT` и `GROUP_CHAT_LOCAL_PORT`. Для остановки выполните:

```bash
docker compose -f compose.local.yml down
```

Тома сохраняются. Конфигурация и сценарии запуска описаны в
[SDD-046](specs/046-local-compose.md).

### Запуск с NVIDIA GPU

На компьютере с видеокартой NVIDIA, доступной Docker, добавьте GPU-оверлей:

```bash
docker compose -f compose.local.yml -f compose.local.gpu.yml up --build --wait --wait-timeout 1200
docker compose -f compose.local.yml -f compose.local.gpu.yml exec ollama ollama ps
```

После загрузки модели в столбце `PROCESSOR` команды `ollama ps` должно быть
`100% GPU`. Если список пуст, сначала отправьте запрос Бобби или выполните
`docker compose -f compose.local.yml -f compose.local.gpu.yml exec ollama ollama run qwen3:8b "Ответь OK"`,
затем повторите `ollama ps`. На тестовом сервере Qwen3 8B показала `100% GPU`.

## Тесты

Для полного прогона нужны Docker, Go, Node.js и PowerShell:

```powershell
pwsh -File scripts/test-coverage.ps1
```

Скрипт запускает Go-тесты с отдельными PostgreSQL и Redis, проверяет фронтенд
и сохраняет отчёты о покрытии в системную временную папку
`bobby-test-coverage`. Путь можно изменить параметром `-OutputDir`.
Результаты и требования к покрытию приведены в [отчёте](TEST_REPORT.md) и
[SDD-060](specs/060-submission-test-coverage.md).

## Серверный стенд

Тестовый стенд на сервере запущен из `D:\HackChange\workspace\main` как проект
`bobby-local`. После сборки фронтенда (`npm ci` и `npm run build` в папке
`frontend`) запустите его из корня клона:

```powershell
docker compose -p bobby-local -f compose.local.yml -f compose.local.gpu.yml -f compose.main-server.yml up -d --build --wait --wait-timeout 1200
```

`compose.local.gpu.yml` включает NVIDIA GPU. `compose.main-server.yml` —
локальный файл сервера: он публикует gateway в Tailscale и использует
`Dockerfile.gateway.prebuilt` для заранее собранного фронтенда. Этот файл
не входит в репозиторий. Для другого распределённого тестового стека есть
[отдельная инструкция](DEPLOY_MOCK_STACK.md).

## Исходные данные и API

`cmd/server` обслуживает чат и поиск по документам. Jira, календарь, групповой
чат и сведения о команде обслуживаются отдельными сервисами. Описание прав,
редактирования моков и MCP-инструментов находится в
[спецификации интеграций](specs/030-mock-only-integrations.md),
[спецификации редакторов](specs/054-mock-data-editors.md) и
[спецификации группового чата](specs/033-group-chat-mcp.md).

API чата создаёт асинхронные задания через `POST /v1/jobs`, возвращает их
состояние через `GET /v1/jobs/{id}` и передаёт обновления по
`/v1/jobs/{id}/ws`. Поиск документов доступен через
`POST /v1/knowledge/search`, переиндексация — через
`POST /v1/knowledge/reindex`. Контракты описаны в
[SDD-002](specs/002-async-requests-websocket.md) и
[SDD-001](specs/001-playbook-rag.md).
