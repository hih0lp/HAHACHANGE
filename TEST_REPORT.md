# Отчёт об автоматических тестах

Проверено на Windows-сервере 10 ноября 2026 года, Go 1.23. Исходный код находится в ветке `codex/rag-integration`.

## Команды

```shell
go test -count=1 -coverpkg=./... -coverprofile=coverage-new.out ./...
go tool cover -func=coverage-new.out
go vet ./...
```

## Результат

- Все Go-тесты прошли.
- Суммарное покрытие прикладного Go-кода: **90,0%** по `go tool cover -func`.
- `go vet ./...` прошёл.
- JavaScript веб-страницы прошёл синтаксическую проверку Node.js.
- Dockerfile запускает Go-тесты при сборке и отклоняет образ при покрытии ниже 90%.

Веб-страница и ответы модели проверяются автоматическими тестами в `cmd/server/web_test.go`, `cmd/server/main_test.go`, `cmd/server/jira_mcp_test.go` и `cmd/server/error_paths_test.go`. Временный профиль `coverage-new.out` создаётся на сервере указанной командой и не добавляется в Git.

Запуск обновлённого контейнера и ручной запрос к модели ожидают восстановления Docker Desktop на сервере.
