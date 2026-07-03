<h1 align="center">CyberVerse</h1>
<p align="center"><em>CyberVerse는 오픈소스 <strong>실시간 오디오/비디오 Agent 플랫폼</strong>입니다. WebRTC, persona memory, tools, RAG, 선택적 디지털 휴먼 비디오 기능을 기반으로 음성 상호작용 중심의 AI Agent를 구축하도록 돕습니다.</em></p>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a> · <a href="README.ja.md">日本語</a> · <a href="README.ko.md"><strong>한국어</strong></a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-GPL%20v3-blue.svg" alt="License: GPL v3"/></a>
  <a href="https://github.com/dsd2077/CyberVerse/pulls"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs Welcome"/></a>
  <a href="https://deepwiki.com/dsd2077/CyberVerse"><img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki" /></a>
  <a href="https://x.com/dsd2077"><img src="https://img.shields.io/badge/@dsd2077-black?logo=x&logoColor=white" alt="X" /></a>
</p>

<p align="center">
  <a href="docs/assets/logo.png"><img src="docs/assets/logo.png" alt="CyberVerse logo" width="100%"/></a>
</p>

---

### 사진 한 장으로 살아 움직이는 디지털 휴먼.

> 당신을 진짜로 보고, 듣고, 실시간으로 말을 건네는 나만의 J.A.R.V.I.S. 같은 AI를 꿈꿔본 적이 있나요?
>
> 그리운 사람을 다시 보고, 그 목소리를 듣고, 미소 짓는 모습을 볼 수 있다면 어떨까요?
>
> 혹은 늘 현실로 불러오고 싶었던 캐릭터가 있을지도 모릅니다.
>
> **사진 한 장이면 됩니다. CyberVerse가 그 존재를 살아 움직이게 합니다.**

## 디지털 휴먼 Agent란?

<p align="center">
  <a href="docs/assets/digital-human-agent.jpeg"><img src="docs/assets/digital-human-agent.jpeg" alt="CyberVerse 디지털 휴먼 Agent" width="100%"/></a>
</p>

## 데모
<p align="center"><em>아래 캐릭터는 데모 예시일 뿐입니다. CyberVerse에 내장되어 제공되지 않으며, 상업적 용도로 제공되지 않습니다.</em></p>

<p align="center">
  <a href="docs/assets/character1.png"><img src="docs/assets/character1.png" alt="CyberVerse 캐릭터 선택 갤러리" width="100%"/></a>
</p>

<p align="center">
  <a href="docs/assets/character2.png"><img src="docs/assets/character2.png" alt="CyberVerse 캐릭터 예시 갤러리" width="100%"/></a>
</p>

<div align="center">

| [![](docs/assets/爱丽丝.mov.png)](https://youtu.be/Lk88sew2x4o) | [![](docs/assets/丽娜.mov.png)](https://youtu.be/8jdQ3ThcwgA) |
|:---:|:---:|
| [**Alice — YouTube에서 보기**](https://youtu.be/Lk88sew2x4o) | [**Lina — YouTube에서 보기**](https://youtu.be/8jdQ3ThcwgA) |

| [![](docs/assets/小龙女.mov.png)](https://youtu.be/WjEHUYZx5Gs) |
|:---:|
| [**Xiaolongnü — YouTube에서 보기**](https://youtu.be/WjEHUYZx5Gs) |

</div>

## 주요 기능

### 실시간 음성 Agent

음성은 CyberVerse의 기본 상호작용 방식이며, 낮은 지연 시간으로 오래 이어갈 수 있는 실시간 대화를 위해 설계되었습니다. 사용자는 마이크로 Agent와 연속적으로 대화하고, 모델이 말하는 중에도 언제든 끊어 말할 수 있으며, 같은 대화 턴에서 음성과 텍스트 입력을 함께 사용할 수 있습니다.

각 캐릭터마다 목소리, 환영 메시지, 성격 설정을 따로 구성할 수 있고, 음성 클로닝도 지원합니다. 대화 중 세션 중단과 재개를 지원하며, `inference.avatar.enabled` 를 `false` 로 설정하면 플랫폼은 순수 음성 모드로 실행되고 오디오 스트림만 publish합니다. 로컬 Avatar GPU가 필요 없고, 핵심 음성 경험은 그대로 유지됩니다.

### WebRTC 기반 오디오/비디오

세션 경로는 WebRTC 기반으로 구성되며, 배포 환경에 따라 direct P2P(내장 TURN / NAT traversal) 또는 LiveKit SFU 모드를 선택할 수 있습니다. 낮은 지연 시간과 복잡한 네트워크 환경에서의 연결성을 함께 고려합니다.

standard 모드 및 지원되는 omni 세션에서 Agent는 사용자 카메라 화면이나 화면 공유 프레임을 visual input으로 받을 수도 있습니다. 순수 텍스트 문맥에만 머무르지 않고, 듣고 볼 수 있는 face-to-face 상호작용을 구현합니다.

### PersonaAgent + SubAgent Tasks

CyberVerse는 multi-agent 아키텍처를 사용합니다. PersonaAgent는 항상 전면에서 사용자와의 부드러운 대화, 빠른 interrupt 대응, context switching을 담당합니다. 검색, 리서치, 자료 정리, 요약, HTML 리포트 생성처럼 시간이 오래 걸리는 작업은 백그라운드 SubAgent가 비동기로 실행합니다.

따라서 복잡한 작업이 음성 턴을 늦추지 않습니다. 사용자는 계속 말하거나, 후속 질문을 하거나, 방향을 조정할 수 있으며, SubAgent가 완료되면 결과가 전면 대화로 다시 전달됩니다.

### 캐릭터 메모리와 RAG

각 캐릭터의 대화 기록은 로컬 디스크에 영속화되고, 다시 대화에 들어가면 자동으로 로드되어 세션을 넘나드는 연속성을 유지합니다. 캐릭터를 위한 지식 베이스, 문서, 인물 생애 자료를 가져올 수도 있습니다. 시스템은 이를 인덱싱해 retrieval-augmented generation에 사용하고, 답변이 캐릭터 배경과 설정에 더 잘 맞도록 합니다.

### 선택적 디지털 휴먼 비디오

GPU 리소스가 있고 Agent를 보이는 존재로 만들고 싶다면 avatar inference를 활성화합니다. 캐릭터 참조 이미지 한 장만으로 FlashHead, LiveAct 같은 구성 가능한 backend를 통해 실시간 얼굴 애니메이션, 립싱크, cached idle video 재생을 구동할 수 있습니다. GPU가 없거나 아직 비디오가 필요하지 않다면 이 기능을 끄고 순수 음성 Agent로 돌아갈 수 있으며, 같은 캐릭터와 persona 설정은 계속 사용할 수 있습니다.

### 플러그인 기반 스택

두뇌, 음성, 청각, 도구, 메모리, 얼굴은 모두 교체 가능한 모듈입니다. `config/cyberverse.yaml` 에서 omni model, LLM, TTS, ASR, Embedding, RAG, tool calls, Avatar backend를 조합하고, Web UI의 **`/settings`** 에서 벤더별 API Key와 서비스 엔드포인트를 설정할 수 있습니다. 시나리오에 따라 provider와 model 조합을 자유롭게 바꿀 수 있습니다.

## 빠른 시작

### 사전 준비

- Node 18+
- Go 1.25 (`protoc-gen-go`, `protoc-gen-go-grpc` 필요)
- Conda
- Python 3.10+
- FFmpeg
- libopus-dev、libopusfile-dev、libsoxr-dev，pkg-config

> 순수 음성 세션에는 로컬 Avatar GPU가 필요하지 않습니다. 실행 비용은 설정한 실시간 음성 / omni / LLM / TTS / ASR provider에 따라 달라집니다.

다음 명령으로 확인할 수 있습니다:

```bash
node --version
go version
protoc --version
ffmpeg -version
conda --version
```

### 1단계: 클론

```bash
git clone https://github.com/dsd2077/CyberVerse.git
cd CyberVerse
```

### 2단계: Python 환경 만들기

```bash
conda create -n cyberverse python=3.10
conda activate cyberverse
```

### 3단계: 환경 변수 설정

```bash
cp -r infra/config config
```

`config/env`를 편집해 지원되는 API Key를 입력합니다.

Alibaba Cloud Qwen 시리즈 모델:

```env
DASHSCOPE_API_KEY=your_dashscope_api_key
```

또는 Volcengine Doubao 시리즈 모델:

```env
DOUBAO_ACCESS_TOKEN=your_doubao_access_token
DOUBAO_APP_ID=your_doubao_app_id
```

Doubao Voice: [Volcengine 빠른 시작](https://www.volcengine.com/docs/6561/2119699?lang=zh)에 따라 **App ID** / **API Key**를 확인하고 `DOUBAO_APP_ID` / `DOUBAO_ACCESS_TOKEN`에 넣습니다.

스택이 실행된 뒤에는 `config/env`만 수정할 필요 없이, Web UI의 **`/settings`** 에서 API 키와 서비스 엔드포인트를 변경할 수 있습니다.

### 4단계: 로컬 설정 생성 및 voice-only 모드 활성화

`config/cyberverse.yaml`을 편집합니다:

```yaml
inference:
  avatar:
    enabled: false
```

`enabled: false`이면 CyberVerse는 순수 음성 Agent assistant로 실행됩니다.

### 5단계: 프로젝트 의존성 설치

```bash
make setup
```

이 단계에서는 기본 editable package(`.[dev,inference]`)를 설치하고, gRPC stubs를 생성하며, 프런트엔드 의존성도 함께 설치합니다.

기본 설정에 필요한 voice-agent extras를 설치합니다:

```bash
# 모든 optional 그룹 한 번에 설치
pip install -e ".[all]"
```

### 6단계: 서비스 시작(터미널 3개)

**터미널 1** — Python 추론 서버:

```bash
conda activate cyberverse
make inference
```

**터미널 2** — Go API 서버:

```bash
make server
```

**터미널 3** — 프런트엔드:

```bash
make frontend
```

### 7단계: 확인

```bash
# API 상태 확인
curl -s http://localhost:8080/api/v1/health
```

브라우저에서 http://localhost:5173 를 엽니다.

## 선택 사항: 전체 디지털 휴먼 비디오

FlashHead 또는 LiveAct로 실시간 Avatar 비디오를 구동하려면 아래 절차를 따르세요.

### 추가 요구 사항

- CUDA 12.8+ 지원 GPU
- PyTorch 2.8(CUDA 12.8)
- `libvpx`가 포함된 FFmpeg(영상 인코딩용)
- Avatar 모델 가중치

PyTorch(CUDA 12.8)를 설치합니다:

```bash
pip3 install torch==2.8.0 torchvision==0.23.0 torchaudio==2.8.0 --index-url https://download.pytorch.org/whl/cu128
```

LiveAct를 사용한다면 vllm을 설치합니다:

```bash
pip install vllm==0.11.0
```

### 모델 가중치 다운로드

CyberVerse는 현재 **FlashHead**와 **LiveAct**를 지원합니다. 필요한 것만 다운로드하면 됩니다. 앞으로 더 많은 모델을 계속 연결할 예정입니다.

```bash
pip install "huggingface_hub[cli]"
```

#### FlashHead（SoulX-FlashHead）

| 모델 구성 요소 | 설명 | 링크 |
| :--- | :--- | :--- |
| `SoulX-FlashHead-1_3B` | 1.3B FlashHead 가중치 | [Hugging Face](https://huggingface.co/Soul-AILab/SoulX-FlashHead-1_3B), [ModelScope](https://modelscope.cn/models/Soul-AILab/SoulX-FlashHead-1_3B) |
| `wav2vec2-base-960h` | 오디오 특징 추출기 | [Hugging Face](https://huggingface.co/facebook/wav2vec2-base-960h), [ModelScope](https://modelscope.cn/models/facebook/wav2vec2-base-960h) |

```bash
# 중국 본토에서는 먼저 미러를 사용할 수 있습니다:
# export HF_ENDPOINT=https://hf-mirror.com

hf download Soul-AILab/SoulX-FlashHead-1_3B \
  --local-dir ./checkpoints/SoulX-FlashHead-1_3B

hf download facebook/wav2vec2-base-960h \
  --local-dir ./checkpoints/wav2vec2-base-960h
```

#### LiveAct（SoulX-LiveAct）

| 모델명 | 다운로드 |
|-----------|----------|
| SoulX-LiveAct | [Hugging Face](https://huggingface.co/Soul-AILab/LiveAct), [ModelScope](https://modelscope.cn/models/Soul-AILab/LiveAct) |
| chinese-wav2vec2-base | [Hugging Face](https://huggingface.co/TencentGameMate/chinese-wav2vec2-base), [ModelScope](https://modelscope.cn/models/TencentGameMate/chinese-wav2vec2-base) |

```bash
hf download Soul-AILab/LiveAct \
  --local-dir ./checkpoints/LiveAct

hf download TencentGameMate/chinese-wav2vec2-base \
  --local-dir ./checkpoints/chinese-wav2vec2-base
```

### Avatar Inference 설정

`config/cyberverse.yaml`에서 `enabled`를 `true`로 설정합니다. 모델별 설정은
`config/avatar_models/` 아래에 모델마다 하나의 YAML 파일로 두고, 그 파일에서 로컬
checkpoint 경로를 수정합니다:

```yaml
inference:
  avatar:
    enabled: true
    default: "flash_head"
    idle_strategy: "silent_inference"
    runtime:
      cuda_visible_devices: 0      # 공용 GPU ID. 멀티 GPU라면 0,1 등으로 설정
      world_size: 1                # 공용 GPU 수. 듀얼 GPU면 2로 설정
    model_config_dir: "avatar_models"
```

그다음 `config/avatar_models/flash_head.yaml` 또는 `config/avatar_models/live_act.yaml`을 수정합니다.
모델 파라미터는 나중에 Web UI에서도 조정할 수 있으며, 해당 모델 설정 파일에 다시 저장됩니다.

### LiveAct FP4 GEMM(선택 사항)

FP4 가속을 사용하려면 [LightX2V](https://github.com/ModelTC/LightX2V)에서 `lightx2v_kernel`을 빌드해 설치해야 합니다. 빌드 머신에는 **PyTorch 2.7+**와 CUTLASS 소스가 필요합니다.

#### 준비

```bash
pip install scikit_build_core uv
```

#### whl 빌드

```bash
git clone https://github.com/NVIDIA/cutlass.git
git clone https://github.com/ModelTC/LightX2V.git
cd LightX2V/lightx2v_kernel
# /path/to/cutlass를 로컬 cutlass 클론의 절대 경로로 바꿉니다.
MAX_JOBS=$(nproc) && CMAKE_BUILD_PARALLEL_LEVEL=$(nproc) \
uv build --wheel \
    -Cbuild-dir=build . \
    -Ccmake.define.CUTLASS_PATH=/path/to/cutlass \
    --verbose \
    --color=always \
    --no-build-isolation
```

#### whl 설치

```bash
pip install dist/*.whl --force-reinstall --no-deps
```

#### CyberVerse에서 활성화

`config/avatar_models/live_act.yaml`(또는 Web UI)의 `live_act`에서 다음을 설정합니다:

```yaml
fp8_gemm: false
fp4_gemm: true
```

플래그를 변경한 뒤 추론 서비스를 재시작하세요.

### SageAttention 및 FlashAttention(선택 사항)

```bash
# SageAttention(소스 빌드)
git clone https://github.com/thu-ml/SageAttention.git
cd SageAttention
export EXT_PARALLEL=4 NVCC_APPEND_FLAGS="--threads 8" MAX_JOBS=32 # Optional
python setup.py install
```

```bash
# FlashAttention (optional)
wget -O flash_attn-2.8.1+cu12torch2.8cxx11abiTRUE-cp312-cp312-linux_x86_64.whl \
  "https://github.com/Dao-AILab/flash-attention/releases/download/v2.8.1/flash_attn-2.8.1%2Bcu12torch2.8cxx11abiTRUE-cp312-cp312-linux_x86_64.whl"

pip install flash_attn-2.8.1+cu12torch2.8cxx11abiTRUE-cp312-cp312-linux_x86_64.whl
```

### Avatar 하드웨어 벤치마크

실시간 디지털 휴먼 비디오에는 GPU 가속이 필요합니다. 아래는 FlashHead 및 LiveAct 아바타 모델의 벤치마크입니다.

| 모델 | 품질 | GPU | 수량 | 해상도 | FPS | 실시간 가능? |
|-------|---------|-----|-------|------------|-----|------------|
| FlashHead 1.3B | Pro | RTX 5090 | 2 | 512×512 | 25+ | ✅ 예 |
| FlashHead 1.3B | Pro | RTX 5090 | 1 | 464x464 | 20 | ✅ 예 |
| FlashHead 1.3B | Pro | RTX PRO 6000 | 1 | 512×512 | 20 | ✅ 예 |
| FlashHead 1.3B | Pro | RTX 4090 | 1 | 512×512 | ~10.8 | ❌ 아니오 |
| FlashHead 1.3B | Lite | RTX 4090 | 1 | 512×512 | 25+ | ✅ 예 |
| LiveAct 18B | — | RTX PRO 6000 | 2 | 320×480 | 20 | ✅ 예 |
| LiveAct 18B | — | RTX PRO 6000 | 1 | 256×417 | 20 | ✅ 예 |

> **Pro**는 화질을, **Lite**는 속도를 우선합니다. 표의 구성은 일반적인 **화질–연산** 균형 예시입니다. 여유 연산이 있으면 화질을 더 올릴 수 있고, 부족하면 해상도·**Pro**/**Lite** 선택 등 화질 설정을 낮춰 실시간 동작을 유지하세요.

avatar inference가 활성화되면 `make inference`는 `config/cyberverse.yaml`의 `inference.avatar.default`를 읽고, 현재 추론 프로세스에서는 그 하나의 아바타 모델만 초기화합니다. 다음 로그가 나올 때까지 기다립니다.

- `Active avatar model initialized: <model_name>`
- `CyberVerse Inference Server started on port 50051`

## 자주 묻는 질문 — 자가 점검(QA)

아바타 영상이 **끊기거나, 멈추거나, 음성보다 눈에 띄게 늦어질** 때는 먼저 추론이 재생 속도를 따라갈 수 있는지 확인하세요.

### 추론 로그로 RTP 확인

**RTP**(실시간 성능 계수)는 chunk 생성에 걸린 시간과, 해당 chunk를 설정 FPS로 재생하는 데 필요한 시간의 비율입니다.

```text
RTP = elapsed / (frames / fps)
```

| RTP | 의미 |
|-----|------|
| **&lt; 1** | 생성이 재생보다 빠름 — 실시간 스트리밍 여유 있음 |
| **= 1** | 정확히 실시간 |
| **&gt; 1** | 생성이 재생보다 느림 — **산출 속도가 소비 속도를 따라가지 못해** 지연·끊김이 발생하기 쉬움 |

캐릭터가 말하는 동안 추론 터미널(`make inference`) 로그에서 **LiveAct** 또는 **FlashHead** chunk 줄을 확인합니다.

**LiveAct 예시(RTP &gt; 1 — 실시간 불가):**

```text
INFO:inference.plugins.avatar.live_act_plugin:LiveAct chunk: idx=2 frames=32 320x480 fps=20 iter=2 elapsed=1.870s is_final=False
```

- 이 chunk 재생 시간: `32 / 20 = 1.6` 초  
- RTP: `1.870 / 1.6 ≈ 1.17` (**&gt; 1** — 현재 GPU에서 320×480 @ 20 fps를 따라가지 못함)

**FlashHead**도 `elapsed`와 `num_frames`, `fps`로 동일하게 계산합니다.

```text
INFO:...FlashHead video chunk generated: chunk_index=1 num_frames=33 512x512 fps=20 ... elapsed=2.100s
```

이 경우 RTP = `2.100 / (33/20) ≈ 1.27`로 실시간을 초과합니다.

### RTP &gt; 1일 때 조치

1. **해상도 또는 화질 낮추기** — 예: LiveAct `infer_params.size`, FlashHead `height` / `width`, 또는 FlashHead `model_type: "lite"`.
2. **연산 자원 늘리기** — GPU 추가(`runtime.world_size`, `cuda_visible_devices`), 지원 시 FP8/FP4 GEMM·컴파일 가속 활성화, 더 빠른 GPU 사용.
3. **위 벤치마크 표와 맞추기** — [Avatar 하드웨어 벤치마크](#avatar-하드웨어-벤치마크)에서 **실시간?** 이 **예**인 해상도·FPS·GPU 조합을 선택.

순수 음성 모드(`inference.avatar.enabled: false`)는 Avatar RTP와 무관합니다. 음성만 끊기면 네트워크/WebRTC 또는 상류 음성 지연을 의심하고 [원격 접속 참고](#원격-접속-참고)를 보세요.

## 원격 접속 참고

`streaming_mode: direct` 에서 내장 TURN 서버를 사용하는 경우, 브라우저가 서버의 `8443/TCP` 에 접속할 수 있어야 합니다. 페이지는 열리지만 오디오/비디오 연결이 끝내 성립하지 않거나, 서버 로그에 `ICE connection state: failed` 또는 `publish timeout waiting for connection` 이 보이면 먼저 로컬 머신에서 서버 `8443` 포트에 연결 가능한지 확인하세요.

```bash
nc -vz <server-ip> 8443
```

`8443` 에 연결되지 않는다면 보통 클라우드 보안 그룹, 방화벽, 또는 NAT 제한이 원인입니다. 이 경우 SSH 터널로 로컬 `8443` 을 서버로 포워딩할 수 있습니다.

```bash
ssh -L 8443:127.0.0.1:8443 user@host -p port
```

터널이 만들어지면 브라우저는 로컬 `127.0.0.1:8443` 을 통해 원격 TURN 서비스에 접속합니다.

SSH 터널이 아니라 브라우저가 원격 서버에 직접 연결되게 하려면 `config/cyberverse.yaml` 의 `pipeline.ice_public_ip` 를 서버의 공인 IP 또는 도메인으로 설정하세요. SSH 터널을 사용할 경우에는 기본값(`127.0.0.1`)을 그대로 사용하면 됩니다.

## 로드맵

### 1. **실시간 오디오/비디오 Agent 플랫폼**

voice-first 실시간 Agent를 더 쉽게 실행하고, 커스터마이즈하고, 임베드할 수 있게 합니다.

- [x] 여러 참조 이미지, 활성 이미지, 고정/랜덤 표시 모드, 선택적 얼굴 크롭, 태그, 음성 필드, 성격, 환영 메시지, 시스템 프롬프트를 포함한 캐릭터 CRUD
- [x] WebRTC 기반 실시간 음성 세션. direct P2P(내장 TURN) 또는 LiveKit SFU
- [x] `inference.avatar.enabled: false` 를 통한 순수 음성 세션
- [x] omni model, LLM, TTS, ASR, Embedding, RAG, avatar를 플러그인으로 제공하며 YAML 및 UI settings로 벤더별 API 키 설정 가능
- [x] 세션 관리: 캐릭터별 대화 기록을 디스크에 영속화하고 대화 시작 시 로드
- [x] 음성 클로닝: 도우바오 음성 클로닝 지원
- [x] 음성과 텍스트 혼합 입력 지원
- [x] 모델 발화 중 음성 끊기 및 세션 중단/재개
- [x] standard 모드 및 지원되는 omni 세션에서 사용자 카메라 입력과 화면 공유 visual frame 지원
- [x] PersonaAgent 및 백그라운드 SubAgent task 실행
- [x] 지식·문서·인물 생애 등 자료를 가져와 캐릭터에 맞춘 RAG 기반 답변
- [ ] 개발자용 웹사이트 임베드(Web 컴포넌트 또는 SDK), 자체 배포 인스턴스를 자체 사이트에 연결
- [ ] 라이브 방송용 음성·영상 스트리밍

### 2. **실시간 디지털 휴먼 통화**

Avatar GPU 리소스가 있을 때 voice Agent를 실시간 영상 통화로 바꿉니다.

- [x] 참조 이미지를 기반으로 구성 가능한 Avatar 플러그인(FlashHead, LiveAct 등)으로 실시간 아바타 영상 구동
- [x] 캐릭터 presence를 위한 cached idle video 재생
- [x] 실시간 speaking segment의 오디오/비디오 동기화
- [ ] 품질, 지연, 비용의 tradeoff가 다른 Avatar backend 추가
- [ ] consumer GPU, workstation GPU, cloud GPU 환경을 위한 더 나은 Avatar 배포 프로파일

### 3. **에이전트 네트워크**

여러 에이전트를 연결해 서로 소통하고 협업하며 네트워크를 형성할 수 있게 합니다.

- [ ] agent-to-agent 통신 활성화
- [ ] 멀티 에이전트 협업 및 위임 활성화
- [ ] 에이전트 간 공유 메모리 및 공유 지식 활성화
- [ ] 연결된 에이전트의 개방형 네트워크 구축

## 커뮤니티

<p align="center">
  <a href="docs/assets/wechat_group.jpg"><img src="docs/assets/wechat_group.jpg" alt="CyberVerse WeChat 그룹 QR 코드" width="320"/></a>
</p>

<p align="center">QR 코드가 만료된 경우 관리자 WeChat <strong>wx_dsd2077</strong>에 추가하고, 친구 신청 시 <strong>CyberVerse</strong>라고 남겨 주세요. 그룹으로 초대해 드립니다.</p>

## Star History

<p align="center">
  <a href="https://star-history.com/#dsd2077/CyberVerse&Date">
    <img src="https://api.star-history.com/svg?repos=dsd2077/CyberVerse&type=Date" alt="Star History Chart" width="100%"/>
  </a>
</p>

## 라이선스

GNU General Public License v3.0. 자세한 내용은 [LICENSE](LICENSE)를 참고하세요.

## 감사의 말

- [SoulX-FlashHead](https://github.com/Soul-AILab/SoulX-FlashHead) — Soul AI Lab의 아바타 모델

- [SoulX-LiveAct](https://github.com/Soul-AILab/SoulX-LiveAct) - Soul AI Lab의 아바타 모델
- [MuseTalk](https://github.com/TMElyralab/MuseTalk) — TME Lyra Lab의 실시간 립싱크 모델
- [Pion](https://github.com/pion/webrtc) — Go WebRTC 구현체
- [Linux.do](https://linux.do/)
