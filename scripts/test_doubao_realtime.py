#!/usr/bin/env python3
"""
Test Doubao Realtime Conversation API end-to-end.

Sends a synthetic sine-wave audio and checks:
  1. WebSocket connection succeeds
  2. StartConnection (event=1) + ConnectionStarted (event=50) handshake OK
  3. StartSession (event=100) + SessionStarted (event=150) OK
  4. ASR transcript comes back
  5. LLM token comes back
  6. TTS audio bytes come back

Run from repo root:
    python scripts/test_doubao_realtime.py
"""

import asyncio
import gzip
import json
import math
import os
import struct
import sys
import uuid

# ── Load .env ────────────────────────────────────────────────────────────────
def load_env():
    env_path = os.path.join(os.path.dirname(__file__), "..", ".env")
    env = {}
    try:
        with open(env_path) as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, _, v = line.partition("=")
                    env[k.strip()] = v.strip()
    except FileNotFoundError:
        pass
    for k in ("DOUBAO_ACCESS_TOKEN", "DOUBAO_APP_ID"):
        if k in os.environ:
            env[k] = os.environ[k]
    return env


# ── Doubao binary protocol (mirrors doubao_protocol.py) ──────────────────────
MSGTYPE_FULL_CLIENT   = 0b0001
MSGTYPE_AUDIO_ONLY    = 0b0010
MSGTYPE_FULL_SERVER   = 0b1001
MSGTYPE_SERVER_ACK    = 0b1011
MSGTYPE_SERVER_ERR    = 0b1111

SERIALIZATION_JSON = 0b0001
SERIALIZATION_RAW  = 0b0011
COMPRESSION_GZIP   = 0b0001
COMPRESSION_NONE   = 0b0000

# DoubaoEvent constants
EVENT_START_CONNECTION  = 1
EVENT_CONNECTION_STARTED = 50
EVENT_START_SESSION     = 100
EVENT_SESSION_STARTED   = 150
EVENT_SESSION_FAILED    = 153
EVENT_SEND_AUDIO        = 200
EVENT_TTS_SENTENCE_DONE = 350
EVENT_REPLY_DONE        = 352


def _encode_frame(msg_type, serialization, compression, event, payload: bytes,
                  session_id: str = "") -> bytes:
    header = bytearray()
    header.append(0x11)                          # version=1, header_size nibble=1 → 4 bytes
    header.append((msg_type << 4) | 0x00)        # msg_type | flags=0
    header.append((serialization << 4) | compression)
    header.append(0x00)                          # reserved
    # event (4-byte big-endian int)
    header += struct.pack(">i", event)
    if session_id:
        sid = session_id.encode()
        header += struct.pack(">I", len(sid)) + sid
    header += struct.pack(">I", len(payload))
    header += payload
    return bytes(header)


def _compress(data: bytes) -> bytes:
    return gzip.compress(data)


def _decode_frame(data: bytes) -> dict:
    if len(data) < 4:
        return {}
    msg_flags      = data[1]
    serial_compress = data[2]
    msg_type       = (msg_flags >> 4) & 0x0F
    serialization  = (serial_compress >> 4) & 0x0F
    compression    = serial_compress & 0x0F

    # Skip fixed 4-byte header; event field follows in some frames
    payload = data[4:]
    if compression == COMPRESSION_GZIP:
        try:
            payload = gzip.decompress(payload)
        except Exception:
            pass

    result = {"msg_type": msg_type}
    if serialization == SERIALIZATION_JSON:
        try:
            result["json"] = json.loads(payload)
        except Exception:
            result["raw_text"] = payload[:200]
    else:
        result["audio"] = payload
    return result


# ── Synthetic 3s 440 Hz sine at 16 kHz s16le ─────────────────────────────────
def make_sine_pcm(duration_s=3.0, freq=440, sr=16000) -> bytes:
    n = int(duration_s * sr)
    buf = bytearray(n * 2)
    for i in range(n):
        s = int(32767 * 0.6 * math.sin(2 * math.pi * freq * i / sr))
        struct.pack_into("<h", buf, i * 2, s)
    return bytes(buf)


CHUNK_BYTES = 3200   # 100 ms of 16 kHz s16le mono


# ── Main test ─────────────────────────────────────────────────────────────────
async def test(env: dict) -> bool:
    import websockets

    access_token = env.get("DOUBAO_ACCESS_TOKEN", "")
    app_id       = env.get("DOUBAO_APP_ID", "")
    ws_url       = "wss://openspeech.bytedance.com/api/v3/realtime/dialogue"

    if not access_token or not app_id:
        print("❌ DOUBAO_ACCESS_TOKEN or DOUBAO_APP_ID not found in .env")
        return False

    session_id = str(uuid.uuid4())
    connect_id = str(uuid.uuid4())

    headers = {
        "X-Api-App-ID":      app_id,
        "X-Api-Access-Key":  access_token,
        "X-Api-Resource-Id": "volc.speech.dialog",
        "X-Api-App-Key":     "PlgvMymc7f3tQnJ6",
        "X-Api-Connect-Id":  connect_id,
    }

    results = {
        "connected":         False,
        "connection_started": False,
        "session_started":   False,
        "asr_transcript":    None,
        "llm_token":         None,
        "tts_audio_bytes":   0,
        "error_code":        None,
        "error_msg":         None,
    }

    print(f"Connecting to {ws_url} …")

    try:
        async with websockets.connect(ws_url, additional_headers=headers, open_timeout=15) as ws:
            results["connected"] = True
            print("✅ WebSocket connected")

            # Step 1 – StartConnection
            conn_payload = _compress(b"{}")
            await ws.send(_encode_frame(
                MSGTYPE_FULL_CLIENT, SERIALIZATION_JSON, COMPRESSION_GZIP,
                EVENT_START_CONNECTION, conn_payload,
            ))
            print("   Sent StartConnection (event=1)")

            # Step 2 – Wait ConnectionStarted (event=50)
            raw = await asyncio.wait_for(ws.recv(), timeout=10)
            frame = _decode_frame(raw)
            results["connection_started"] = True
            print(f"✅ ConnectionStarted: {frame.get('json', {})}")

            # Step 3 – StartSession
            session_cfg = {
                "asr": {
                    "extra": {"end_smooth_window_ms": 4000},
                    "audio_config": {"channel": 1, "format": "pcm", "sample_rate": 16000},
                },
                "tts": {
                    "speaker": "zh_female_qingxin",
                    "audio_config": {"channel": 1, "format": "pcm", "sample_rate": 16000},
                },
                "dialog": {
                    "extra": {"strict_audit": False},
                },
            }
            sess_payload = _compress(json.dumps(session_cfg).encode())
            await ws.send(_encode_frame(
                MSGTYPE_FULL_CLIENT, SERIALIZATION_JSON, COMPRESSION_GZIP,
                EVENT_START_SESSION, sess_payload, session_id=session_id,
            ))
            print("   Sent StartSession (event=100)")

            # Step 4 – Wait SessionStarted (event=150)
            raw = await asyncio.wait_for(ws.recv(), timeout=10)
            frame = _decode_frame(raw)
            j = frame.get("json", {})
            if j.get("event") == EVENT_SESSION_FAILED or frame.get("msg_type") == MSGTYPE_SERVER_ERR:
                results["error_code"] = j.get("code") or j.get("error_code")
                results["error_msg"]  = j.get("message") or str(j)
                print(f"❌ SessionFailed: {j}")
                return _report(results)
            results["session_started"] = True
            print(f"✅ SessionStarted: {j}")

            # Step 5 – Stream audio + read responses concurrently
            pcm = make_sine_pcm()

            async def send_audio():
                for off in range(0, len(pcm), CHUNK_BYTES):
                    chunk = pcm[off:off + CHUNK_BYTES]
                    frame = _encode_frame(
                        MSGTYPE_AUDIO_ONLY, SERIALIZATION_RAW, COMPRESSION_NONE,
                        EVENT_SEND_AUDIO, chunk, session_id=session_id,
                    )
                    await ws.send(frame)
                    await asyncio.sleep(0.09)
                # EOS
                eos = _encode_frame(
                    MSGTYPE_AUDIO_ONLY, SERIALIZATION_RAW, COMPRESSION_NONE,
                    EVENT_SEND_AUDIO, b"", session_id=session_id,
                )
                await ws.send(eos)
                print("   Sent EOS")

            send_task = asyncio.create_task(send_audio())

            deadline = asyncio.get_event_loop().time() + 25
            async for raw_msg in ws:
                f = _decode_frame(raw_msg if isinstance(raw_msg, bytes) else raw_msg.encode())
                mt = f.get("msg_type")
                j  = f.get("json", {})

                if mt == MSGTYPE_SERVER_ERR:
                    results["error_code"] = j.get("code") or j.get("error_code")
                    results["error_msg"]  = j.get("message") or str(j)
                    print(f"❌ Server error {results['error_code']}: {results['error_msg']}")
                    break

                if mt == MSGTYPE_FULL_SERVER and j:
                    event   = j.get("event", 0)
                    payload = j.get("payload_msg") or {}
                    text    = (payload.get("text") or payload.get("result", {}).get("text", "")
                               if isinstance(payload, dict) else "")

                    if event in (1, 2, 301) and text and results["asr_transcript"] is None:
                        results["asr_transcript"] = text
                        print(f"   ASR transcript: {text!r}")
                    elif event in (50, 51, 150, 151, 250, 251) and text:
                        if results["llm_token"] is None:
                            results["llm_token"] = text
                            print(f"   LLM token: {text!r}")
                    elif event in (EVENT_TTS_SENTENCE_DONE, EVENT_REPLY_DONE):
                        print(f"   Reply done (event={event})")
                        break
                    else:
                        print(f"   event={event} payload={str(payload)[:80]}")

                elif "audio" in f and len(f["audio"]) > 0:
                    prev = results["tts_audio_bytes"]
                    results["tts_audio_bytes"] += len(f["audio"])
                    if prev == 0:
                        print(f"   TTS audio arriving …")

                if asyncio.get_event_loop().time() > deadline:
                    print("⚠️  25s deadline reached")
                    break

            send_task.cancel()

    except websockets.InvalidStatus as e:
        status = getattr(getattr(e, "response", None), "status_code", "?")
        print(f"❌ WebSocket rejected: HTTP {status} — check credentials / headers")
        return False
    except Exception as e:
        print(f"❌ Connection error: {type(e).__name__}: {e}")
        return False

    return _report(results)


def _report(results: dict) -> bool:
    ok = lambda v: "✅" if v else "❌"
    print("\n" + "=" * 55)
    print("DOUBAO REALTIME API TEST RESULTS")
    print("=" * 55)
    print(f"{ok(results['connected'])}  WebSocket connected")
    print(f"{ok(results['connection_started'])}  StartConnection handshake OK")
    print(f"{ok(results['session_started'])}  StartSession OK (no auth/config error)")
    print(f"{ok(results['asr_transcript'])}  ASR transcript: {results['asr_transcript']!r}")
    print(f"{ok(results['llm_token'])}  LLM token: {results['llm_token']!r}")
    print(f"{ok(results['tts_audio_bytes'] > 0)}  TTS audio received: {results['tts_audio_bytes']} bytes")
    if results["error_code"]:
        print(f"\n  Error {results['error_code']}: {results['error_msg']}")

    all_ok = (
        results["connected"]
        and results["session_started"]
        and results["tts_audio_bytes"] > 0
    )
    verdict = "✅ Doubao API WORKING" if all_ok else "❌ Doubao API has issues"
    print("\n" + verdict)
    return all_ok


if __name__ == "__main__":
    env = load_env()
    ok = asyncio.run(test(env))
    sys.exit(0 if ok else 1)

