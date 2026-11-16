# Бобби — помощник стажёра

## Запуск из скачанного репозитория

Скачайте репозиторий кнопкой **Code → Download ZIP** на GitHub и распакуйте
архив либо клонируйте репозиторий. Откройте терминал в каталоге с
`compose.yaml`. Нужны запущенный Docker Desktop с Docker Compose,
интернет для первой загрузки и около 12 ГБ памяти, доступной Docker.

```bash
docker compose up --build --wait --wait-timeout 1200
```

Откройте [чат Бобби](http://127.0.0.1:18001/) или
[демомессенджер](http://127.0.0.1:28085/). В профиле чата доступны два
стажёра и наставник. Для проверки спросите «Какие у меня задачи в Jira?»,
затем «В каких группах мессенджера я состою?».

Compose собирает React и Go из исходников, применяет миграции, заполняет
учебные сервисы и загружает публичный AvitoTech Playbook. Файл `.env`,
локальные Go и Node.js и соседние каталоги для этого запуска не требуются.
Первый запуск скачивает образы и Qwen3 8B объёмом около 5,2 ГБ.

Модель фиксирована как `qwen3:8b`. Настройки генерации находятся в
`deploy/Modelfile.qwen3-8b`: temperature 0, top_k 20, top_p 0.95,
repeat_penalty 1. Контекст составляет 8192 токена, Ollama выполняет один
запрос за раз. Прогрев модели происходит при первом вопросе. На CPU
первый ответ может занять несколько минут. Контейнеры API и мессенджера
дожидаются готовности модели эмбеддингов. Docker автоматически выбирает
образ эмбеддингов для x86-64 или ARM64, включая Apple Silicon.

Модели, история чатов и изменения моков сохраняются в Docker-томах
проекта `bobby-download`. Порты можно изменить переменными
`BOBBY_LOCAL_PORT` и `GROUP_CHAT_LOCAL_PORT`. Остановить стенд можно так:

```bash
docker compose down
```

### NVIDIA GPU

При доступной Docker видеокарте NVIDIA:

```bash
docker compose -f compose.yaml -f compose.local.gpu.yml up --build --wait --wait-timeout 1200
```

После первого вопроса команда
`docker compose exec ollama ollama ps` показывает, где выполняется модель.
На Mac базовый запуск использует CPU внутри Linux-контейнера Docker.

### Ранее развёрнутые стенды

`compose.local.yml` сохраняет конфигурацию проекта `bobby-local` для ранее
развёрнутого стенда. Его запуск описан в [SDD-046](specs/046-local-compose.md).
Порядок запуска скачанного репозитория описан в
[SDD-061](specs/061-repository-download.md).

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
