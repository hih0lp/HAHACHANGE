"""Check GitLab MCP discovery and project reads using the demo intern binding."""
import json
import subprocess
import urllib.request
from pathlib import Path


def main():
    config = {}
    for line in (Path(__file__).resolve().parents[1] / ".env").read_text(encoding="utf-8").splitlines():
        if "=" in line and not line.startswith("#"):
            key, value = line.split("=", 1)
            config[key] = value.strip().strip("'\"")
    request = urllib.request.Request(
        "http://127.0.0.1:18080/v1/auth/login",
        data=json.dumps({"username": "intern", "password": config["TEAM_ACCESS_INTERN_PASSWORD"]}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        token = json.load(response)["access_token"]
    shell = """
read -r token
read -r body
wget -S -O - --timeout=15 --header="Authorization: Bearer $token" \
  --header="Content-Type: application/json" --header="Accept: application/json, text/event-stream" \
  --header="MCP-Protocol-Version: 2025-03-26" --post-data="$body" "$GITLAB_MCP_URL"
"""

    def rpc(method, params):
        payload = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
        result = subprocess.run(
            ["docker", "exec", "-i", "avito-rag-distributed-worker-1", "sh", "-c", shell],
            input=(token + "\n" + payload + "\n").encode(),
            capture_output=True, timeout=25,
        )
        if result.returncode:
            statuses = [line.strip() for line in result.stderr.decode(errors="replace").splitlines() if line.strip().startswith("HTTP/")]
            raise RuntimeError(f"{method}: {', '.join(statuses) or 'transport failed'}")
        body = json.loads(result.stdout)
        if "error" in body or body.get("result", {}).get("isError"):
            raise RuntimeError(f"{method} returned an MCP error")
        return body["result"]

    tools = {tool["name"]: tool for tool in rpc("tools/list", {})["tools"]}
    if len(tools) != 14 or "gitlab_projects" not in tools:
        raise RuntimeError(f"unexpected GitLab tool catalogue: {len(tools)} tools")
    required = tools["gitlab_projects"]["inputSchema"].get("required", [])
    arguments = {"project": "2"} if "project" in required else {}
    projects = rpc("tools/call", {"name": "gitlab_projects", "arguments": arguments})["structuredContent"]["data"]
    if not isinstance(projects, list):
        raise RuntimeError("GitLab projects result is not a list")
    print(f"GitLab MCP OK: {len(tools)} tools, {len(projects)} visible projects on the first page")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        status = getattr(error, "code", None)
        detail = f" (HTTP {status})" if isinstance(status, int) else ""
        if isinstance(error, RuntimeError):
            detail = f": {error}"
        raise SystemExit(f"GitLab MCP check failed: {type(error).__name__}{detail}") from None
