<h1 align="center">CyberVerse</h1>
<p align="center"><em>CyberVerse 是一个开源的<strong>实时数字人 Agent 框架</strong>。它基于 WebRTC、人设记忆、工具、RAG 和可选的数字人视频能力，帮助你构建以语音交互为核心的 AI Agent。</em></p>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md"><strong>简体中文</strong></a> · <a href="README.ja.md">日本語</a> · <a href="README.ko.md">한국어</a>
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

### 一张照片，让数字人真正「活」起来。

> 你是否想过拥有一个属于自己的 J.A.R.V.I.S.——能真正看见你、听见你、陪伴你的 AI？
>
> 想再次见到思念之人，听见 TA 的声音，看见 TA 对你微笑？
>
> 又或者，你一直想把某个角色带到现实世界中？
>
> **只需一张照片，CyberVerse 就能让 TA 「活」过来。**

## 什么是数字人 Agent？

<p align="center">
  <a href="docs/assets/digital-human-agent.jpeg"><img src="docs/assets/digital-human-agent.jpeg" alt="CyberVerse 数字人 Agent" width="100%"/></a>
</p>

## 演示
<p align="center"><em>以下角色仅用于 Demo 演示，不会随 CyberVerse 内置提供，也不用于商业用途。</em></p>
<p align="center">
  <a href="docs/assets/character1.png"><img src="docs/assets/character1.png" alt="CyberVerse 角色选择界面" width="100%"/></a>
</p>

<p align="center">
  <a href="docs/assets/character2.png"><img src="docs/assets/character2.png" alt="CyberVerse 角色示例界面" width="100%"/></a>
</p>

<div align="center">

| [![](docs/assets/爱丽丝.mov.png)](https://youtu.be/Lk88sew2x4o) | [![](docs/assets/丽娜.mov.png)](https://youtu.be/8jdQ3ThcwgA) |
|:---:|:---:|
| [**爱丽丝 — 在 YouTube 观看**](https://youtu.be/Lk88sew2x4o) | [**丽娜 — 在 YouTube 观看**](https://youtu.be/8jdQ3ThcwgA) |

| [![](docs/assets/小龙女.mov.png)](https://youtu.be/WjEHUYZx5Gs) |
|:---:|
| [**小龙女 — 在 YouTube 观看**](https://youtu.be/WjEHUYZx5Gs) |

</div>

## 功能特性

### 实时语音 Agent

语音是 CyberVerse 的默认交互方式，面向低延迟、可长时间进行的实时对话。用户可以通过麦克风与 Agent 连续交流，在模型说话时随时打断，也可以在同一轮会话中混合使用语音和文本输入。

每个角色可单独配置声线、欢迎语与人格设定，并支持语音克隆。对话过程中支持会话中断与恢复；将 `inference.avatar.enabled` 设为 `false` 时，平台会以纯语音模式运行，只发布音频流，无需本地 Avatar GPU，核心语音体验保持不变。

### 基于 WebRTC 的音视频

会话链路基于 WebRTC 构建，可按部署场景选择直连 P2P（内嵌 TURN / NAT 穿透）或 LiveKit SFU 模式，兼顾低延迟与复杂网络环境下的连通性。

在 standard 模式及受支持的 omni 会话中，Agent 还可以接收用户摄像头画面或屏幕共享帧作为视觉输入，实现「能听、能看」的面对面式交互，而不局限于纯文本上下文。

### PersonaAgent + SubAgent 任务

CyberVerse 采用multi-agent架构：PersonaAgent 始终驻守前台，负责与用户保持流畅对话、快速响应打断和上下文切换；搜索、调研、资料整理、总结以及 HTML 报告生成等耗时工作则交给后台 SubAgent 异步执行。

这样复杂任务不会拖慢语音回合，用户可以继续说话、追问或调整方向，待 SubAgent 完成后再把结果回传给前台对话。

### 角色记忆与 RAG

每个角色的会话历史会持久化到本地磁盘，重新进入对话时会自动加载，保证跨会话的连续感。你还可以为角色导入知识库、文档和人物生平类素材，系统会建立索引并用于检索增强生成，让回答更贴合角色背景与设定。

### 可选数字人视频

当你具备 GPU 资源并希望 Agent「可见」时，可开启 avatar inference：只需一张角色参考图，即可通过 FlashHead、LiveAct 等可配置后端驱动实时面部动画、口型同步，并在不说话时播放缓存的待机视频。没有 GPU 或暂时不需要视频时，关闭该能力即可退回纯语音 Agent，同一套角色与人设配置仍可继续使用。

### 插件化技术栈

大脑、声音、听觉、工具、记忆和面孔均为可替换模块。运行行为仍放在 `config/cyberverse.yaml`，omni 模型、LLM、TTS、ASR、Embedding 的 provider 定义会从内置 `infra/config/*_models/` 目录自动加载，也支持在 `config/*_models/` 下放置本地覆盖文件。你可以在 Web UI 的 **`/settings`** 中配置不同厂商的 API Key 与服务端点，按场景自由切换供应商与模型组合。

## 快速开始

### 前置条件

- Node 18+
- Go 1.25（需安装：`protoc-gen-go`、`protoc-gen-go-grpc`）
- Conda
- Python 3.10+
- FFmpeg
- libopus-dev、libopusfile-dev、libsoxr-dev，pkg-config

> 纯语音会话不需要本地 Avatar GPU。运行成本取决于你配置的实时语音 / omni / LLM / TTS / ASR 服务提供商。

可用以下命令验证：

```bash
node --version
go version
protoc --version
ffmpeg -version
conda --version
```

### 第 1 步：克隆仓库

```bash
git clone https://github.com/dsd2077/CyberVerse.git
cd CyberVerse
```

### 第 2 步：创建 Python 环境

```bash
conda create -n cyberverse python=3.10
conda activate cyberverse
```

### 第 3 步：配置环境变量

```bash
cp -r infra/config config
```

编辑 `config/env`，填入支持的API key：
aliyun Qwen系列模型

```env
DASHSCOPE_API_KEY=your_dashscope_api_key
```

或者火山引擎：Doubao系列模型：

```env
DOUBAO_ACCESS_TOKEN=your_doubao_access_token
DOUBAO_APP_ID=your_doubao_app_id
```

豆包语音：按照 [火山引擎快速入门](https://www.volcengine.com/docs/6561/2119699?lang=zh) 获取 **App ID** / **API Key**，并填入 `DOUBAO_APP_ID` / `DOUBAO_ACCESS_TOKEN`。

服务启动后，你也可以在 Web UI 的 **`/settings`** 页面修改 API Key 和服务端点，而不必只依赖编辑 `config/env`。

omni、LLM、Embedding、TTS、ASR 的模型定义会从 `infra/config/*_models/` 自动发现；只有需要本地覆盖时，才在 `config/*_models/` 下放置同名模型文件。

### 第 4 步：创建本地配置并启用 voice-only 模式

编辑 `config/cyberverse.yaml`：

```yaml
inference:
  avatar:
    enabled: false
```

当 `enabled: false` 时，CyberVerse 会作为纯语音 Agent 助手运行。

### 第 5 步：安装项目依赖

```bash
make setup
```

这一步会安装基础可编辑包（`[dev,inference]`）、生成 gRPC stubs，并安装前端依赖。

安装默认配置所需的语音 Agent extras：

```bash
# 一次安装全部可选组
pip install -e ".[all]"
```

### 第 6 步：启动服务（3 个终端）

**终端 1** — Python 推理服务：

```bash
conda activate cyberverse
make inference
```

**终端 2** — Go API 服务：

```bash
make server
```

**终端 3** — 前端：

```bash
make frontend
```

### 第 7 步：验证

```bash
# 检查 API 健康状态
curl -s http://localhost:8080/api/v1/health
```

在浏览器中打开 http://localhost:5173。

## 可选：完整数字人视频模式

如果你希望用 FlashHead 或 LiveAct 驱动实时 Avatar 视频，请按如下步骤执行。

### 额外要求

- 支持 CUDA 12.8+ 的 GPU
- PyTorch 2.8（CUDA 12.8）
- FFmpeg（需包含 `libvpx`，用于视频编码）
- Avatar 模型权重

安装 PyTorch（CUDA 12.8）：

```bash
pip3 install torch==2.8.0 torchvision==0.23.0 torchaudio==2.8.0 --index-url https://download.pytorch.org/whl/cu128
```

如果使用 LiveAct，安装 vllm：

```bash
pip install vllm==0.11.0
```

### 下载模型权重

CyberVerse 目前支持 **FlashHead** 与 **LiveAct**，按需下载即可；后续会继续接入更多模型。

```bash
pip install "huggingface_hub[cli]"
```

#### FlashHead（SoulX-FlashHead）

| 模型组件 | 说明 | 链接 |
| :--- | :--- | :--- |
| `SoulX-FlashHead-1_3B` | 1.3B FlashHead 权重 | [Hugging Face](https://huggingface.co/Soul-AILab/SoulX-FlashHead-1_3B), [ModelScope](https://modelscope.cn/models/Soul-AILab/SoulX-FlashHead-1_3B) |
| `wav2vec2-base-960h` | 音频特征提取器 | [Hugging Face](https://huggingface.co/facebook/wav2vec2-base-960h), [ModelScope](https://modelscope.cn/models/facebook/wav2vec2-base-960h) |

```bash
# 如果你在中国大陆，可以先使用镜像：
# export HF_ENDPOINT=https://hf-mirror.com

hf download Soul-AILab/SoulX-FlashHead-1_3B \
  --local-dir ./checkpoints/SoulX-FlashHead-1_3B

hf download facebook/wav2vec2-base-960h \
  --local-dir ./checkpoints/wav2vec2-base-960h
```

#### LiveAct（SoulX-LiveAct）

| 模型名称 | 下载 |
|-----------|----------|
| SoulX-LiveAct | [Hugging Face](https://huggingface.co/Soul-AILab/LiveAct), [ModelScope](https://modelscope.cn/models/Soul-AILab/LiveAct) |
| chinese-wav2vec2-base | [Hugging Face](https://huggingface.co/TencentGameMate/chinese-wav2vec2-base), [ModelScope](https://modelscope.cn/models/TencentGameMate/chinese-wav2vec2-base) |

```bash
hf download Soul-AILab/LiveAct \
  --local-dir ./checkpoints/LiveAct

hf download TencentGameMate/chinese-wav2vec2-base \
  --local-dir ./checkpoints/chinese-wav2vec2-base
```

### 配置 Avatar Inference

在 `config/cyberverse.yaml` 中将 `enabled` 设为 `true`。具体模型参数放在
`config/avatar_models/` 下，每个模型一个 YAML 文件；把对应文件里的路径改成你的本地
checkpoint 路径。

```yaml
inference:
  avatar:
    enabled: true
    default: "flash_head"               # 可选 "flash_head" 或 "live_act"
    idle_strategy: "silent_inference"
    runtime:
      cuda_visible_devices: 0      # 共享 GPU ID，例如多卡可写 0,1
      world_size: 1                # 共享 GPU 数量，双卡时设为 2
    model_config_dir: "avatar_models"
```

然后编辑当前模型文件，例如 `config/avatar_models/flash_head.yaml` 或
`config/avatar_models/live_act.yaml`。这些模型参数之后也可以在 Web UI 中调整，并会写回
对应的模型配置文件。

### 百度曦灵 H5 数字人

使用百度曦灵时，把密钥放在 `config/env`：

```env
BAIDU_XILING_APP_ID="your-app-id"
BAIDU_XILING_APP_KEY="your-app-key"
# 如果形象需要固定机位，可选配置。
BAIDU_XILING_CAMERA_ID="0"
```

百度曦灵在 Web UI 中按角色选择。它不是本地 avatar inference 模型，不应配置为
`inference.avatar.default`。CyberVerse 仍通过 orchestrator 处理 ASR、LLM、TTS、
历史上下文和角色设定，然后把 16 kHz、16-bit、单声道 PCM 音频分片发送到浏览器。
前端嵌入百度 H5 iframe，并按官方 `sendAudioData` / `AUDIO_STREAM_RENDER` 消息格式
驱动数字人。

### LiveAct FP4 GEMM（可选）

FP4 加速需从 [LightX2V](https://github.com/ModelTC/LightX2V) 编译安装 `lightx2v_kernel`。环境需 **PyTorch 2.7+**，并在本机准备好 CUTLASS 源码。

#### 准备

```bash
pip install scikit_build_core uv
```

#### 编译 whl

```bash
git clone https://github.com/NVIDIA/cutlass.git
git clone https://github.com/ModelTC/LightX2V.git
cd LightX2V/lightx2v_kernel
# 将 /path/to/cutlass 改为你本机 cutlass 仓库的绝对路径。
MAX_JOBS=$(nproc) && CMAKE_BUILD_PARALLEL_LEVEL=$(nproc) \
uv build --wheel \
    -Cbuild-dir=build . \
    -Ccmake.define.CUTLASS_PATH=/path/to/cutlass \
    --verbose \
    --color=always \
    --no-build-isolation
```

#### 安装 whl

```bash
pip install dist/*.whl --force-reinstall --no-deps
```

#### 在 CyberVerse 中开启

在 `config/avatar_models/live_act.yaml`（或 Web UI）的 `live_act` 下设置：

```yaml
fp8_gemm: false
fp4_gemm: true
```

修改后请重启推理服务。

### SageAttention 和 FlashAttention（可选）

```bash
# SageAttention（源码编译）
git clone https://github.com/thu-ml/SageAttention.git
cd SageAttention
export EXT_PARALLEL=4 NVCC_APPEND_FLAGS="--threads 8" MAX_JOBS=32 # Optional
python setup.py install
```

```bash
# FlashAttention（可选）
wget -O flash_attn-2.8.1+cu12torch2.8cxx11abiTRUE-cp312-cp312-linux_x86_64.whl \
  "https://github.com/Dao-AILab/flash-attention/releases/download/v2.8.1/flash_attn-2.8.1%2Bcu12torch2.8cxx11abiTRUE-cp312-cp312-linux_x86_64.whl"

pip install flash_attn-2.8.1+cu12torch2.8cxx11abiTRUE-cp312-cp312-linux_x86_64.whl
```

### Avatar 硬件基准

实时数字人视频需要 GPU 加速。下表为 FlashHead 和 LiveAct Avatar 模型的性能基准：

| 模型 | 档位 | GPU | 数量 | 分辨率 | FPS | 实时运行？ |
|-------|---------|-----|-------|------------|-----|------------|
| FlashHead 1.3B | Pro | RTX 5090 | 2 | 512×512 | 25+ | ✅ 是 |
| FlashHead 1.3B | Pro | RTX 5090 | 1 | 464x464 | 20 | ✅ 是 |
| FlashHead 1.3B | Pro | RTX PRO 6000 | 1 | 512×512 | 20 | ✅ 是 |
| FlashHead 1.3B | Pro | RTX 4090 | 1 | 512×512 | ~10.8 | ❌ 否 |
| FlashHead 1.3B | Lite | RTX 4090 | 1 | 512×512 | 25+ | ✅ 是 |
| LiveAct 18B | — | RTX PRO 6000 | 2 | 320×480 | 20 | ✅ 是 |
| LiveAct 18B | — | RTX PRO 6000 | 1 | 256×417 | 20 | ✅ 是 |

> **Pro** 偏重画质；**Lite** 偏重速度。表中配置体现画质与算力的大致平衡：算力更充裕时可进一步提高画质；算力不足时请降低画质相关选项（分辨率、Pro / Lite 档位等）以保持实时流畅。

Avatar inference 启用后，`make inference` 会读取 `config/cyberverse.yaml` 中的 `inference.avatar.default`，并且只在当前推理进程中初始化该一个 Avatar 模型。等待日志中出现：

- `Active avatar model initialized: <model_name>`
- `CyberVerse Inference Server started on port 50051`

## 常见问题自检（QA）

当数字人视频出现**卡顿、画面冻结或明显落后于语音**时，可先按下面步骤自检，确认推理是否跟得上播放。

### 用推理日志检查 RTP

**RTP**（实时性能系数）表示：生成一段视频 chunk 实际耗时，与该 chunk 按配置 FPS 播放所需时长的比值：

```text
RTP = elapsed / (frames / fps)
```

| RTP | 含义 |
|-----|------|
| **&lt; 1** | 生成快于播放，有余量，可稳定实时推流 |
| **= 1** | 刚好实时 |
| **&gt; 1** | 生成慢于播放，**产出速度跟不上消耗速度**，容易积压、卡顿 |

在角色说话时查看推理终端（`make inference`）日志，关注 **LiveAct** 或 **FlashHead** 的 chunk 行。

**LiveAct 示例（RTP &gt; 1，无法实时）：**

```text
INFO:inference.plugins.avatar.live_act_plugin:LiveAct chunk: idx=2 frames=32 320x480 fps=20 iter=2 elapsed=1.870s is_final=False
```

- 该 chunk 播放时长：`32 / 20 = 1.6` 秒  
- RTP：`1.870 / 1.6 ≈ 1.17`（**&gt; 1**，说明当前 GPU 在 320×480 @ 20 fps 下跟不上）

**FlashHead** 同理，用 `elapsed` 与 `num_frames`、`fps` 计算：

```text
INFO:...FlashHead video chunk generated: chunk_index=1 num_frames=33 512x512 fps=20 ... elapsed=2.100s
```

此处 RTP = `2.100 / (33/20) ≈ 1.27`，同样超出实时。

### RTP &gt; 1 时的处理建议

1. **降低分辨率或画质档位** — 例如调低 LiveAct 的 `infer_params.size`、FlashHead 的 `height` / `width`，或将 FlashHead 设为 `model_type: "lite"`。
2. **增加算力** — 增加 GPU（`runtime.world_size`、`cuda_visible_devices`），在支持时开启 FP8/FP4 GEMM 或编译加速，或换更快的显卡。
3. **对照上方基准表** — 选择 [Avatar 硬件基准](#avatar-硬件基准) 中 **实时运行？** 为「是」的分辨率、FPS 与 GPU 组合。

纯语音模式（`inference.avatar.enabled: false`）不涉及 Avatar RTP；若仅语音卡顿，多为网络/WebRTC 或上游语音链路问题，可参考 [远程访问说明](#远程访问说明)。

## 远程访问说明

当 `streaming_mode: direct` 且使用内嵌 TURN 时，浏览器必须能够访问服务端的 `8443/TCP`。如果页面可以打开，但音视频始终无法建立连接，或者服务端日志中出现 `ICE connection state: failed`、`publish timeout waiting for connection`，请先在本机检查与服务器 `8443` 端口是否连通：

```bash
nc -vz <server-ip> 8443
```

如果 `8443` 不可达，通常是云安全组、防火墙或 NAT 限制导致。此时可以通过 SSH 隧道将本机 `8443` 转发到服务器：

```bash
ssh -L 8443:127.0.0.1:8443 user@host -p port
```

建立隧道后，浏览器会通过本机 `127.0.0.1:8443` 转发访问远端 TURN 服务。

如果你不是通过 SSH 隧道访问，而是希望浏览器直接连接远端服务器，请将 `config/cyberverse.yaml` 中的 `pipeline.ice_public_ip` 设置为服务器的公网 IP 或域名；如果使用 SSH 隧道，可以保持默认值（`127.0.0.1`）。

## 路线图

路线图已迁移至语雀：[CyberVerse需求管理](https://www.yuque.com/u32995802/ilet4r/qu7lhylertuzx7dh?singleDoc#)。

## 社区交流

<p align="center">
  <a href="docs/assets/wechat_group.jpg"><img src="docs/assets/wechat_group.jpg" alt="CyberVerse 微信技术交流群二维码" width="320"/></a>
</p>

<p align="center">如果二维码过期，可添加本人微信：<strong>wx_dsd2077</strong>，并备注 <strong>CyberVerse</strong>，我会邀请您进群</p>

## 星标历史

<p align="center">
  <a href="https://star-history.com/#dsd2077/CyberVerse&Date">
    <img src="https://api.star-history.com/svg?repos=dsd2077/CyberVerse&type=Date" alt="Star History Chart" width="100%"/>
  </a>
</p>

## 许可证

GNU General Public License v3.0，详见 [LICENSE](LICENSE)。

## 致谢

- [SoulX-FlashHead](https://github.com/Soul-AILab/SoulX-FlashHead) — Soul AI Lab 提供的 Avatar 模型

- [SoulX-LiveAct](https://github.com/Soul-AILab/SoulX-LiveAct) - Soul AI Lab 提供的 Avatar 模型
- [MuseTalk](https://github.com/TMElyralab/MuseTalk) — TME Lyra Lab 提供的实时口型同步模型
- [Pion](https://github.com/pion/webrtc) — Go WebRTC 实现
- [Linux.do](https://linux.do/)
