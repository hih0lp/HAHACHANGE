"""A/B probe of the old and new LLM routing stages, without calling MCP.

Run through an SSH tunnel to the isolated LM Studio endpoint. No credentials
are read, and the fixed clock makes calendar plans comparable.
"""

from __future__ import annotations

import json
import re
import subprocess
import sys
import time
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ENDPOINT = "http://127.0.0.1:2234/v1/chat/completions"
CLOCK = "2026-11-13T15:00:00+03:00"
CASES = [
    ("jira", "Для SCRUM-7 скажи статус, срок исполнения и дату начала", ["jira"]),
    ("calendar", "Какие мероприятия завтра в онбординге?", ["calendar"]),
    ("rights", "Хочу получить доступ к джире, куда заявку отправить?", ["rights"]),
    ("documents", "Что в документации по GitLab сказано о ревью?", ["documents"]),
    ("mixed", "Что завтра по онбордингу и какие задачи в Jira?", ["calendar", "jira"]),
    ("recommendation", "Что мне подучить с учетом моих задач Jira, MR GitLab, команды и документации?", ["jira", "gitlab", "team", "documents"]),
]


def source(path: str, old: bool) -> str:
    if old:
        return subprocess.check_output(["git", "show", f"b0dbd40:{path}"], cwd=ROOT, text=True)
    return (ROOT / path).read_text()


def instruction(code: str, name: str) -> str:
    match = re.search(rf"(?:const {name}|{name} :=) =?\s*(`[^`]*`|\"[^\"\n]*\")", code)
    if match is None:
        raise ValueError(f"missing {name}")
    raw = match.group(1)
    return raw[1:-1] if raw.startswith("`") else json.loads(raw)


def ask(model: str, system: str, user: str) -> tuple[dict, float]:
    request = {"model": model, "stream": False, "temperature": 0,
               "messages": [{"role": "system", "content": system}, {"role": "user", "content": user}]}
    body = json.dumps(request, ensure_ascii=False).encode()
    started = time.monotonic()
    req = urllib.request.Request(ENDPOINT, data=body, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=180) as response:
        result = json.load(response)
    elapsed = time.monotonic() - started
    content = result["choices"][0]["message"]["content"].strip()
    content = re.sub(r"^```(?:json)?\s*|\s*```$", "", content).strip()
    return json.loads(content), elapsed


def main() -> None:
    old_editor = instruction(source("cmd/server/query_filter.go", True), "routeInstruction")
    new_editor = instruction(source("cmd/server/query_filter.go", False), "routeInstruction")
    old_planner = instruction(source("cmd/server/calendar_planner.go", True), "instruction")
    new_planner = instruction(source("cmd/server/calendar_planner.go", False), "instruction")
    results = []
    for name, query, expected in CASES:
        item = {"name": name, "query": query, "expected": expected}
        for version, editor, planner in (("old", old_editor, old_planner), ("new", new_editor, new_planner)):
            try:
                edited, editor_seconds = ask("qwen3-1.7b", editor, query)
                if version == "old":
                    planner_question = query
                else:
                    planner_question = f"Исходное сообщение: {query}\nРедакторская формулировка: {edited['cleaned_question']}"
                plan, planner_seconds = ask("qwen3-8b", planner,
                    f"Current time: {CLOCK}\nAvailable forms: []\nQuestion: {planner_question}")
                item[version] = {"edited": edited.get("cleaned_question"),
                                 "editor_categories": edited.get("categories"),
                                 "planner_categories": plan.get("required_categories"),
                                 "plan_flags": {k: v for k, v in plan.items() if k.startswith("needs_") and v},
                                 "time_min": plan.get("time_min"), "time_max": plan.get("time_max"),
                                 "editor_seconds": round(editor_seconds, 2),
                                 "planner_seconds": round(planner_seconds, 2)}
            except Exception as error:
                item[version] = {"error": str(error)}
            print(f"{name} {version}: {item[version]}", file=sys.stderr, flush=True)
        results.append(item)
        (ROOT / "eval/route_ab_results.json").write_text(json.dumps(results, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
