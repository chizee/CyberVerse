#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ -f ./.env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

WORLD_SIZE="${WORLD_SIZE:-2}"
CUDA_VISIBLE_DEVICES="${CUDA_VISIBLE_DEVICES:-0,1}"
MASTER_ADDR="${MASTER_ADDR:-127.0.0.1}"
MASTER_PORT="${MASTER_PORT:-29500}"
# Force to a safe value (libgomp: Invalid value for environment variable OMP_NUM_THREADS).
export OMP_NUM_THREADS=1

export WORLD_SIZE CUDA_VISIBLE_DEVICES MASTER_ADDR MASTER_PORT OMP_NUM_THREADS

# FlashHead distributed inference is sensitive to short dialog tails.
# Lower the minimum "new audio" ratio so it can emit the first chunk
# before Doubao hits DialogAudioIdleTimeoutError.
if [[ -z "${FLASHHEAD_MIN_NEW_AUDIO_RATIO:-}" ]]; then
  export FLASHHEAD_MIN_NEW_AUDIO_RATIO=0.6
fi

echo "[inference-multi-gpu] FLASHHEAD_MIN_NEW_AUDIO_RATIO=${FLASHHEAD_MIN_NEW_AUDIO_RATIO}"
echo "[inference-multi-gpu] Warmup control: cyberverse_config.yaml -> warmup.*"

# torch.compile can make the first inference too slow for real-time streaming.
# Default to off for better "first video appears" reliability; enable for tuning.
if [[ -z "${FLASHHEAD_COMPILE_MODEL:-}" ]]; then
  export FLASHHEAD_COMPILE_MODEL=0
fi
if [[ -z "${FLASHHEAD_COMPILE_VAE:-}" ]]; then
  export FLASHHEAD_COMPILE_VAE=0
fi
echo "[inference-multi-gpu] FLASHHEAD_COMPILE_MODEL=${FLASHHEAD_COMPILE_MODEL}"
echo "[inference-multi-gpu] FLASHHEAD_COMPILE_VAE=${FLASHHEAD_COMPILE_VAE}"

# In some environments, calling torch.distributed collectives from a Python
# background thread deadlocks. Run the distributed worker loop on the main
# thread for non-rank0 to avoid it.
if [[ -z "${FLASHHEAD_DIST_WORKER_MAIN_THREAD:-}" ]]; then
  export FLASHHEAD_DIST_WORKER_MAIN_THREAD=1
fi
echo "[inference-multi-gpu] FLASHHEAD_DIST_WORKER_MAIN_THREAD=${FLASHHEAD_DIST_WORKER_MAIN_THREAD}"

echo "[inference-multi-gpu] WORLD_SIZE=${WORLD_SIZE} CUDA_VISIBLE_DEVICES=${CUDA_VISIBLE_DEVICES}"
echo "[inference-multi-gpu] MASTER_ADDR=${MASTER_ADDR} MASTER_PORT=${MASTER_PORT}"

./scripts/generate_proto.sh

torchrun \
  --nnodes=1 \
  --nproc_per_node="${WORLD_SIZE}" \
  --master_addr="${MASTER_ADDR}" \
  --master_port="${MASTER_PORT}" \
  -m inference.server --config cyberverse_config.yaml
