#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

CUDA_VISIBLE_DEVICES=0

CUDA_VISIBLE_DEVICES=$CUDA_VISIBLE_DEVICES python "$SCRIPT_DIR/generate_video.py" \
    --ckpt_dir "$PROJECT_ROOT/checkpoints/SoulX-FlashHead-1_3B" \
    --wav2vec_dir "$PROJECT_ROOT/checkpoints/wav2vec2-base-960h" \
    --model_type lite \
    --cond_image "$SCRIPT_DIR/../../examples/girl.png" \
    --audio_path "$SCRIPT_DIR/../../examples/podcast_sichuan_16k.wav" \
    --audio_encode_mode stream
