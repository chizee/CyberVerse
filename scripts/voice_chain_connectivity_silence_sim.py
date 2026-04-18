import json
import os
import subprocess
import time
import urllib.request

import websocket

BASE_HTTP = "http://localhost:8080"
WS_PREFIX = "ws://localhost:8080"


def http_post_json(url: str, payload: dict, timeout_s: int = 30):
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        body = resp.read().decode("utf-8")
        return json.loads(body)


def http_delete(url: str, timeout_s: int = 10):
    req = urllib.request.Request(url, method="DELETE")
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        _ = resp.read()


def main():
    wait_s = int(os.environ.get("VOICE_WAIT_S", "120"))
    silence_s = int(os.environ.get("SILENCE_DURATION_S", "30"))
    pcm_amplitude = int(os.environ.get("SILENCE_PCM_AMPLITUDE", "0"))
    waveform = os.environ.get("SILENCE_WAVEFORM", "noise")  # noise|const

    print(
        f"[silence-sim] wait_s={wait_s} silence_s={silence_s} pcm_amplitude={pcm_amplitude} waveform={waveform}"
    )

    resp = http_post_json(BASE_HTTP + "/api/v1/sessions", {"mode": "voice_llm"})
    session_id = resp.get("session_id")
    livekit_url = resp.get("livekit_url")
    livekit_token = resp.get("livekit_token")

    if not session_id:
        raise RuntimeError(f"missing session_id: {resp}")
    if not livekit_url or not livekit_token:
        raise RuntimeError(f"missing livekit_url/token in resp: {resp}")

    print(f"[silence-sim] session_id={session_id}")
    print(f"[silence-sim] livekit_url={livekit_url}")

    # Publish synthetic silence as the "user" remote participant,
    # so the server bot's SubscribeUserAudio() receives real PCM.
    sim_cmd = [
        "go",
        "run",
        "./cmd/livekit_silence_user",
        "--livekit_url",
        livekit_url,
        "--livekit_token",
        livekit_token,
        "--participant_identity",
        f"user-sim-{session_id[:8]}",
        "--duration_s",
        str(silence_s),
        "--pcm_amplitude",
        str(pcm_amplitude),
        "--waveform",
        waveform,
        "--sample_rate",
        "16000",
        "--chunk_ms",
        "20",
    ]
    sim_proc = subprocess.Popen(
        sim_cmd,
        cwd="/root/CyberVerse/server",
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )

    ws_url = f"{WS_PREFIX}/ws/chat/{session_id}"
    print(f"[silence-sim] connecting ws: {ws_url}")
    ws = websocket.create_connection(ws_url, timeout=10)
    ws.settimeout(5)

    got_status = set()
    got_video_evidence = False
    got_error = None

    started = time.time()
    try:
        while time.time() - started < wait_s:
            try:
                raw = ws.recv()
            except Exception:
                continue
            if not raw:
                continue
            data = json.loads(raw)
            mtype = data.get("type")
            if mtype == "error":
                got_error = data
                print(f"[silence-sim][error] {data}")
                # Don't break immediately: avatar generation may still need time
                # to flush its final video frames after a voice tail error.
                continue
            if mtype == "avatar_status":
                st = data.get("status")
                if st:
                    got_status.add(st)
                print(f"[silence-sim][status] {data}")
            if mtype == "transcript":
                # We don't assert exact transcript; just keep consuming.
                pass

    finally:
        try:
            ws.close()
        except Exception:
            pass

        # Cleanup session
        try:
            http_delete(BASE_HTTP + f"/api/v1/sessions/{session_id}")
        except Exception:
            pass

        # Stop silence simulator (if still running)
        if sim_proc.poll() is None:
            try:
                sim_proc.terminate()
            except Exception:
                pass

    # Best-effort: print last few simulator lines.
    try:
        out = sim_proc.stdout.read().splitlines()[-10:]
        if out:
            print("[silence-sim] simulator tail:")
            for line in out:
                print(line)
    except Exception:
        pass

    print("[silence-sim] done:", {
        "session_id": session_id,
        "got_status": sorted(got_status),
        "got_error": got_error,
        "wait_s": wait_s,
        "silence_s": silence_s,
    })


if __name__ == "__main__":
    main()

