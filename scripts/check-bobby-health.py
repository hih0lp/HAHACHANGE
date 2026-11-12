"""Probe the local Bobby stack and the Tailscale listener from the host."""
import http.cookiejar
import json
import subprocess
import time
import urllib.request
from pathlib import Path


def command(*args):
    return subprocess.run(args, check=True, capture_output=True, timeout=12).stdout.decode().strip()


def main():
    failures = []

    def check(name, operation):
        try:
            print(f"{name}: OK ({operation()})", flush=True)
        except Exception as error:
            status = getattr(error, "code", None)
            detail = f", HTTP {status}" if isinstance(status, int) else ""
            print(f"{name}: FAILED ({type(error).__name__}{detail})", flush=True)
            failures.append(name)

    def docker():
        services = ("gateway", "api", "worker", "postgres", "redis", "kafka", "team-access", "access-rights")
        states = json.loads(command("docker", "inspect", *(f"avito-rag-distributed-{s}-1" for s in services)))
        bad = [entry["Name"].lstrip("/") for entry in states if not entry["State"]["Running"]
               or entry["State"].get("Health", {}).get("Status", "healthy") != "healthy"]
        if bad:
            print("Docker services with failed state: " + ", ".join(bad), flush=True)
            raise RuntimeError("unhealthy services")
        return f"{len(states)} containers"

    def tailnet():
        address = command("tailscale", "ip", "-4").splitlines()[0]
        status = json.loads(command("tailscale", "status", "--json"))
        if status["BackendState"] != "Running" or not status["Self"]["Online"]:
            raise RuntimeError("Tailscale node is offline")
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(f"http://{address}:18000/", timeout=10) as response:
            return f"HTTP {response.status}, host-side tailnet listener"

    config = {}
    for line in (Path(__file__).resolve().parents[1] / ".env").read_text(encoding="utf-8").splitlines():
        if "=" in line and not line.startswith("#"):
            key, value = line.split("=", 1)
            config[key] = value.strip().strip("'\"")
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )
    base = "http://127.0.0.1:18000"

    def api():
        with opener.open(base + "/", timeout=10) as response:
            if response.status != 200:
                raise RuntimeError("chat entry unavailable")
        if not isinstance(job("/v1/chat/history").get("turns"), list):
            raise RuntimeError("invalid chat history response")
        return "HTTP 200 and valid chat history JSON"

    def job(path, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(
            base + path, data=data, headers={"Content-Type": "application/json", "X-API-Key": config["API_KEY"]}
        )
        with opener.open(request, timeout=10) as response:
            return json.load(response)

    def bobby():
        with opener.open(base + "/", timeout=10):
            pass
        created = job("/v1/jobs", {"query": "Сколько будет 19 плюс 23? Ответь одним числом.", "scenario": "general"})
        deadline = time.monotonic() + 100
        while time.monotonic() < deadline:
            result = job("/v1/jobs/" + created["id"])
            if result["status"] == "completed":
                if str(result.get("answer", "")).strip().strip("*. `") != "42":
                    raise RuntimeError("unexpected synthetic answer")
                return "job completed with expected model answer"
            if result["status"] == "failed":
                raise RuntimeError("synthetic job failed")
            time.sleep(2)
        raise TimeoutError("synthetic job did not complete")

    check("Docker", docker)
    check("Remote listener", tailnet)
    check("API", api)
    check("Bobby", bobby)
    return bool(failures)


if __name__ == "__main__":
    raise SystemExit(main())
