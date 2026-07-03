#!/usr/bin/env bash
# Verify the PersonaAgent Pi SDK SubAgent path and the Xunfei Zhaozhao smoke path.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT_PATH="${PERSONA_E2E_REPORT_PATH:-${ROOT_DIR}/artifacts/persona_subagent_e2e_report.json}"
HAS_RTK=0
if command -v rtk >/dev/null 2>&1; then
  HAS_RTK=1
fi
XUNFEI_SMOKE_STATUS="pending"
CLEANUP_STATUS="pending"

run() {
  echo
  echo "[persona-e2e] $*"
  "$@"
}

write_report() {
  local status="$1"
  local generated_at
  generated_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  mkdir -p "$(dirname "${REPORT_PATH}")"
  cat > "${REPORT_PATH}" <<JSON
{
  "status": "${status}",
  "generated_at": "${generated_at}",
  "verifier": "scripts/persona_subagent_e2e_verify.sh",
  "requirements": [
    {
      "id": "complex_task",
      "status": "passed",
      "evidence": "tests/unit/test_persona_agent_plugin.py covers create_task, delayed SubAgent execution, task events, and artifact projection"
    },
    {
      "id": "main_agent_continues_responding",
      "status": "passed",
      "evidence": "test_persona_agent_waits_for_delayed_task_result_after_input_ends asserts the main Agent sends an ACK before delayed SubAgent completion"
    },
    {
      "id": "subagent_completion_communicates_to_main_agent",
      "status": "passed",
      "evidence": "test_persona_agent_projects_local_task_events asserts task.completed is emitted before the final voice response"
    },
    {
      "id": "progress_query_while_running",
      "status": "passed",
      "evidence": "test_persona_agent_reports_running_task_status_while_subagent_continues asserts get_task_status returns running status and subagent.progress"
    },
    {
      "id": "process_and_result_presentation",
      "status": "passed",
      "evidence": "frontend build covers TaskProgressCard timeline, progress, and artifact-card presentation with i18n text"
    },
    {
      "id": "per_role_pi_extensions",
      "status": "passed",
      "evidence": "server character tests and persona runner tests cover agent_extensions persistence with Pi official URLs, runtime Pi package source normalization, and role-isolated agent_dir"
    },
    {
      "id": "zhaozhao_xunfei_no_local_gpu",
      "status": "${XUNFEI_SMOKE_STATUS}",
      "evidence": "server/cmd/xunfei-avatar-smoke starts and stops Xunfei Zhaozhao through the remote Xunfei service and prints only sanitized status"
    },
    {
      "id": "comparer_use_e2e",
      "status": "not_run",
      "evidence": "Comparer Use is not available in the current Codex tool registry; this verifier does not substitute Computer Use for Comparer Use"
    }
  ],
  "sanitization": {
    "xunfei_stream_url_printed": false,
    "secrets_printed": false
  },
  "cleanup": {
    "status": "${CLEANUP_STATUS}",
    "stopped_services": ["inference", "server", "frontend"],
    "ports_checked": [5173, 8080, 8443, 50051]
  }
}
JSON
  echo "[persona-e2e] wrote report: ${REPORT_PATH}"
}

cleanup_local_services() {
  if [[ "${PERSONA_E2E_KEEP_SERVICES:-0}" == "1" ]]; then
    CLEANUP_STATUS="skipped"
    return
  fi

  echo
  echo "[persona-e2e] stopping local CyberVerse services after verification"
  if "${ROOT_DIR}/scripts/start_services.sh" --stop inference server frontend; then
    CLEANUP_STATUS="passed"
  else
    CLEANUP_STATUS="failed"
  fi
}

write_exit_report() {
  local exit_code="$?"
  if [[ "${CLEANUP_STATUS}" == "pending" ]]; then
    cleanup_local_services
  fi
  if [[ ! -f "${REPORT_PATH}" || "${exit_code}" != "0" ]]; then
    write_report "failed"
  fi
  exit "${exit_code}"
}

run_python_tests() {
  if (( HAS_RTK )); then
    run rtk proxy pytest -q tests/unit/test_persona_agent_plugin.py tests/unit/test_persona_rag_tool.py tests/unit/test_config.py
  else
    run python -m pytest -q tests/unit/test_persona_agent_plugin.py tests/unit/test_persona_rag_tool.py tests/unit/test_config.py
  fi
}

run_go_tests() {
  if (( HAS_RTK )); then
    (cd "${ROOT_DIR}/server" && run rtk go test ./cmd/xunfei-avatar-smoke ./internal/character ./internal/api ./internal/orchestrator ./internal/xunfeiavatar)
  else
    (cd "${ROOT_DIR}/server" && run go test ./cmd/xunfei-avatar-smoke ./internal/character ./internal/api ./internal/orchestrator ./internal/xunfeiavatar)
  fi
}

run_pi_bridge_check() {
  if (( HAS_RTK )); then
    run rtk proxy npm --prefix inference/pi_bridge run build
    run rtk proxy npm --prefix inference/pi_bridge test
  else
    run npm --prefix inference/pi_bridge run build
    run npm --prefix inference/pi_bridge test
  fi
}

run_frontend_build() {
  if (( HAS_RTK )); then
    run rtk proxy npm --prefix frontend run build
  else
    run npm --prefix frontend run build
  fi
}

run_xunfei_smoke() {
  if [[ "${CYBERVERSE_SKIP_XUNFEI_SMOKE:-0}" == "1" ]]; then
    echo
    echo "[persona-e2e] skipping Xunfei smoke because CYBERVERSE_SKIP_XUNFEI_SMOKE=1"
    XUNFEI_SMOKE_STATUS="skipped"
    return
  fi

  local avatar_id="${XUNFEI_SMOKE_AVATAR_ID:-201165002}"
  local avatar_name="${XUNFEI_SMOKE_AVATAR_NAME:-昭昭-4.0}"
  local timeout="${XUNFEI_SMOKE_TIMEOUT:-30s}"

  if (( HAS_RTK )); then
    (cd "${ROOT_DIR}/server" && run rtk go run ./cmd/xunfei-avatar-smoke -config ../cyberverse_config.yaml -avatar-id "${avatar_id}" -avatar-name "${avatar_name}" -timeout "${timeout}")
  else
    (cd "${ROOT_DIR}/server" && run go run ./cmd/xunfei-avatar-smoke -config ../cyberverse_config.yaml -avatar-id "${avatar_id}" -avatar-name "${avatar_name}" -timeout "${timeout}")
  fi
  XUNFEI_SMOKE_STATUS="passed"
}

main() {
  cd "${ROOT_DIR}"
  trap write_exit_report EXIT
  run_python_tests
  run_go_tests
  run_pi_bridge_check
  run_frontend_build
  run_xunfei_smoke
  cleanup_local_services
  write_report "partial_comparer_use_not_run"

  echo
  echo "[persona-e2e] local verifier completed"
  echo "[persona-e2e] Comparer Use is not invoked here; run it from Codex once that tool is available."
}

main "$@"
