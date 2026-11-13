"""Offline comparison of source routing using a multilingual cross-encoder.

Only the route catalogue and labelled queries live here; no service token is read.
"""

from __future__ import annotations

import json
import statistics
import sys
import time
from pathlib import Path

from sentence_transformers import CrossEncoder


CATEGORIES = {
    "jira": "Нужны факты из Jira: карточка задачи, исполнитель, статус, срок, дата начала, описание или список задач.",
    "calendar": "Нужны события или свободные слоты единого календаря наставника на указанный период.",
    "gitlab": "Нужны факты из GitLab: доступный проект, исходный код, коммит, merge request, автор или ревьюер.",
    "documents": "Нужны правила, процессы, инструкции или знания из документации и playbook.",
    "rights": "Нужны права доступа, каталог ресурсов, форма заявки или статус запроса на доступ.",
    "team": "Нужен профиль сотрудника, наставник, состав команды, роль или сопоставление личности сотрудника.",
}

CASES = [
    ("Для задачи SCRUM-7 скажи статус, срок исполнения и дату начала", ["jira"]),
    ("Какие у меня задачи в Jira?", ["jira", "team"]),
    ("Какие мероприятия завтра в онбординге?", ["calendar"]),
    ("Хочу получить доступ к джире, куда заявку отправить?", ["rights"]),
    ("Кто мой наставник?", ["team"]),
    ("Какие открытые MR есть в моих проектах GitLab?", ["gitlab"]),
    ("Что в документации сказано про ревью кода?", ["documents"]),
    ("Что мне подучить с учетом моих задач Jira, MR GitLab, команды и документации?", ["jira", "gitlab", "team", "documents"]),
    ("Что завтра по онбордингу и какие задачи в Jira?", ["calendar", "jira"]),
    ("Скажи, как оформить доступ к GitLab и кто в команде согласует заявку", ["rights", "team"]),
    ("Сравни мою задачу SCRUM-7 с правилами ревью из playbook", ["jira", "documents"]),
    ("Назови мои открытые MR и задачи, которые мне назначены", ["gitlab", "jira", "team"]),
    ("Есть ли свободное время у наставника на неделе и кто он?", ["calendar", "team"]),
    ("Опиши содержимое SCRUM-7, а затем покажи форму доступа к Jira", ["jira", "rights"]),
    ("Что означает HTTP 502?", []),
    ("Напиши пример функции на Go для сортировки среза", []),
    ("В документации по GitLab что сказано о ревью?", ["documents"]),
    ("Какая дата дедлайна SCRUM-7? Не показывай календарь", ["jira"]),
    ("Какие события 13 ноября 2026 года и какие слоты наставника свободны?", ["calendar"]),
    ("Есть ли задача коллеги по доступу и кто её исполнитель?", ["jira"]),
    ("Покажи мои навыки по профилю команды и личным MR", ["team", "gitlab"]),
    ("Что нам известно о моих навыках, если в Jira нет описания и MR не назначены?", ["jira", "gitlab", "team"]),
]


def evaluate(scores: list[list[float]], threshold: float) -> dict[str, float]:
    tp = fp = fn = exact = 0
    for (_, expected), row in zip(CASES, scores):
        predicted = {cat for cat, score in zip(CATEGORIES, row) if score >= threshold}
        wanted = set(expected)
        tp += len(predicted & wanted)
        fp += len(predicted - wanted)
        fn += len(wanted - predicted)
        exact += predicted == wanted
    precision = tp / (tp + fp) if tp + fp else 0
    recall = tp / (tp + fn) if tp + fn else 0
    return {"threshold": threshold, "precision": precision, "recall": recall, "exact": exact / len(CASES), "false_negative": fn, "false_positive": fp}


def main() -> None:
    start = time.monotonic()
    model = CrossEncoder("Qwen/Qwen3-Reranker-0.6B", device="cpu", max_length=256)
    load_seconds = time.monotonic() - start
    instruction = "Определи, нужен ли указанный источник фактов для ответа на запрос. Оцени каждую пару независимо; запрос может требовать несколько источников или ни одного."
    scores = []
    timings = []
    for query, _ in CASES:
        begun = time.monotonic()
        pairs = [(f"<Instruct>: {instruction}\n<Query>: {query}", f"<Document>: {description}") for description in CATEGORIES.values()]
        row = [float(x) for x in model.predict(pairs, batch_size=6, show_progress_bar=False)]
        scores.append(row)
        timings.append(time.monotonic() - begun)
        print(f"{len(scores)}/{len(CASES)} {timings[-1]:.1f}s", file=sys.stderr, flush=True)
    output = {"model": "Qwen/Qwen3-Reranker-0.6B", "load_seconds": load_seconds, "mean_route_seconds": statistics.mean(timings), "cases": [
        {"query": query, "expected": expected, "scores": dict(zip(CATEGORIES, row)), "seconds": seconds}
        for (query, expected), row, seconds in zip(CASES, scores, timings)
    ], "thresholds": [evaluate(scores, value) for value in (-5, -2, 0, 2, 5)]}
    target = Path(__file__).with_name("reranker_routes_results.json")
    target.write_text(json.dumps(output, ensure_ascii=False, indent=2), encoding="utf-8")
    print(target)


if __name__ == "__main__":
    main()
