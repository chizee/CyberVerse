import json
import time
import urllib.request

import websocket

BASE_HTTP = "http://localhost:8080"
TEXT = "你好，测试链路连通性（quick）。"


def http_post_json(url: str, payload: dict, timeout_s: int = 20):
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def http_get_json(url: str, timeout_s: int = 10):
    with urllib.request.urlopen(url, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def wait_for_health(timeout_s: int = 20):
    deadline = time.time() + timeout_s
    last = None
    while time.time() < deadline:
        try:
            last = http_get_json(BASE_HTTP + "/api/v1/health")
            if last.get("status") == "ok":
                return last
        except Exception as e:
            last = {"error": str(e)}
        time.sleep(1)
    raise RuntimeError(f"health not ready: {last}")


def main():
    print("[quick] waiting for health...")
    health = wait_for_health(20)
    print(f"[quick] health ok: {health}")

    print("[quick] creating session (standard)...")
    resp = http_post_json(BASE_HTTP + "/api/v1/sessions", {"mode": "standard"})
    session_id = resp.get("session_id")
    if not session_id:
        raise RuntimeError(f"missing session_id: {resp}")
    print(f"[quick] session_id={session_id}")

    ws_url = f"ws://localhost:8080/ws/chat/{session_id}"
    print(f"[quick] connecting ws: {ws_url}")
    ws = websocket.create_connection(ws_url, timeout=10)
    ws.settimeout(5)

    got_llm_token = False
    got_error = None
    got_avatar_status = set()

    try:
        ws.send(json.dumps({"type": "text_input", "text": TEXT}))
        print("[quick] sent text_input")

        deadline = time.time() + 25
        while time.time() < deadline:
            try:
                raw = ws.recv()
            except Exception:
                continue
            if not raw:
                continue
            data = json.loads(raw)
            mtype = data.get("type")
            if mtype == "llm_token":
                got_llm_token = True
            if mtype == "avatar_status":
                st = data.get("status")
                if st:
                    got_avatar_status.add(st)
            if mtype == "error":
                got_error = data
                print(f"[quick][error] {data}")
                break

            if mtype in ("llm_token", "avatar_status"):
                print(f"[quick][ws] {mtype}: {data}")

    finally:
        try:
            ws.close()
        except Exception:
            pass

    print("[quick] result:", {
        "got_llm_token": got_llm_token,
        "got_error": got_error,
        "got_avatar_status": sorted(got_avatar_status),
    })


if __name__ == "__main__":
    main()
