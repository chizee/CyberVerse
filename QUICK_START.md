# CyberVerse Quick Start (Windows)

本文档补充说明如何在 **Windows 10/11** 环境下配置与启动 CyberVerse（含 GPU 推理、Web UI）。

> 建议优先使用 Docker Compose 路线：Windows 上依赖更少、复现更稳定。

## 1. 前置条件

### 必装

- Windows 10/11（建议 Win11）
- [Docker Desktop](https://www.docker.com/products/docker-desktop/)（启用 WSL2 后端）
- NVIDIA 显卡驱动（如需 GPU 推理）

### 可选但推荐

- Miniconda / Anaconda（用于安装 `huggingface-cli` 下载模型权重）
- Git

### 快速自检（PowerShell）

```powershell
docker version
docker compose version
```

GPU 自检（成功会输出一张 `nvidia-smi` 表）：

```powershell
docker run --rm --gpus all nvidia/cuda:12.8.0-base-ubuntu22.04 nvidia-smi
```

## 2. 拉取代码

```powershell
git clone <your_repo_url>
cd CyberVerse
```

## 3. 下载模型权重（FlashHead / LiveAct）

项目默认使用 FlashHead（轻量 1.3B）作为 Avatar 模型。你需要把权重放到仓库根目录的 `checkpoints/` 下，目录结构大致如下：

```text
checkpoints/
  SoulX-FlashHead-1_3B/
  wav2vec2-base-960h/
  LiveAct/                       (可选)
  chinese-wav2vec2-base/          (可选)
```

### 3.1 安装 huggingface-cli

```powershell
python -m pip install -U "huggingface_hub[cli]"
```

如果你在大陆网络环境，建议先设置镜像（只对当前终端生效）：

```powershell
$env:HF_ENDPOINT="https://hf-mirror.com"
```

### 3.2 下载 FlashHead（推荐）

```powershell
huggingface-cli download Soul-AILab/SoulX-FlashHead-1_3B `
  --local-dir .\checkpoints\SoulX-FlashHead-1_3B

huggingface-cli download facebook/wav2vec2-base-960h `
  --local-dir .\checkpoints\wav2vec2-base-960h
```

### 3.3 下载 LiveAct（可选）

```powershell
huggingface-cli download Soul-AILab/LiveAct `
  --local-dir .\checkpoints\LiveAct

huggingface-cli download TencentGameMate/chinese-wav2vec2-base `
  --local-dir .\checkpoints\chinese-wav2vec2-base
```

## 4. 配置 `.env`

在仓库根目录创建/编辑 `.env`（注意不要把真实 Key 提交到 git）：

```dotenv
DOUBAO_ACCESS_TOKEN=你的豆包token
DOUBAO_APP_ID=你的豆包app_id

# 可选：如果你要用 OpenAI 文本对话链路（LLM/TTS），再填这个
# OPENAI_API_KEY=sk-...

# 可选：TURN 密码（直连 WebRTC 的 NAT 穿透用；本地跑也建议填个）
TURN_PASSWORD=cyberverse_turn_dev
```

说明：

- **Voice 模式（Web UI 默认）** 需要 `DOUBAO_ACCESS_TOKEN` + `DOUBAO_APP_ID`。
- **Text 模式**（手动创建 `standard` session）需要 `OPENAI_API_KEY`（除非你自己改成别的 LLM/TTS 插件）。

## 5. Docker Compose 启动（推荐）

从仓库根目录进入 `infra/`，用根目录的 `.env` 启动：

```powershell
cd .\infra
docker compose --env-file ..\.env up -d --build
```

启动后端口：

- Web UI（nginx）：http://localhost
- Go API（server）：http://localhost:8080
- API 健康检查（走 nginx 反代）：http://localhost/api/v1/health

验证（PowerShell 里请用 `curl.exe`，避免命中 `Invoke-WebRequest` 的别名与安全提示）：

```powershell
curl.exe -s http://localhost/api/v1/health
curl.exe -s http://localhost/api/v1/config/avatar-model
```

看日志（排查最常用）：

```powershell
docker compose --env-file ..\.env logs -f cyberverse-server
docker compose --env-file ..\.env logs -f cyberverse-inference
```

## 6. Web UI 使用流程

1. 打开 http://localhost
2. 创建角色（Character）
3. 上传头像图片（Avatar）
4. 点击 Launch 进入会话页（Session）
5. 允许浏览器麦克风权限（Voice 模式需要）

## 7. 常见问题排查

### 7.1 有视频但 bot 不回复

先看推理服务日志：

```powershell
cd .\infra
docker compose --env-file ..\.env logs --tail 200 cyberverse-inference
```

如果看到 `server rejected WebSocket connection: HTTP 401`：

- 这是 **豆包实时语音**鉴权失败，通常是 `DOUBAO_ACCESS_TOKEN / DOUBAO_APP_ID` 不对或已过期。
- 修改 `.env` 后请重建/重启推理容器再试：

```powershell
cd .\infra
docker compose --env-file ..\.env up -d --no-deps --force-recreate cyberverse-inference
```

如果你不想用 Voice 模式，想先验证“文本输入就能回”，可以用 API 创建一个 `standard` session，然后手动打开 session 页面：

```powershell
# 先拿一个 character_id（从 /characters 列表里找）
curl.exe -s http://localhost/api/v1/characters

# 创建 standard session
curl.exe -s -X POST http://localhost/api/v1/sessions `
  -H "Content-Type: application/json" `
  -d "{\"mode\":\"standard\",\"character_id\":\"<CHARACTER_ID>\"}"
```

把返回的 `session_id` 拼到浏览器里打开：

```text
http://localhost/session/<SESSION_ID>?streaming_mode=direct&character_id=<CHARACTER_ID>
```

注意：`standard` 文本链路默认依赖 `OPENAI_API_KEY`（没填就不会有回复；错误目前可能只在日志里看到）。

### 7.2 `docker compose` 提示缺少 DOUBAO 变量

你需要显式指定 env-file：

```powershell
cd .\infra
docker compose --env-file ..\.env up -d
```

### 7.3 PowerShell 的 `curl` 有安全提示/行为不一致

PowerShell 里 `curl` 是 `Invoke-WebRequest` 的别名。建议全程用：

```powershell
curl.exe -s http://localhost/api/v1/health
```

### 7.4 角色数据重启后丢失

目前 `cyberverse-server` 容器内的 `/app/data` 没有默认挂载到宿主机，重建容器会丢数据。

解决方法：编辑 `infra/docker-compose.yml`，给 `cyberverse-server` 增加 volume（示例）：

```yaml
services:
  cyberverse-server:
    volumes:
      - ../data:/app/data
```

然后在仓库根目录创建 `data/` 目录，再重启：

```powershell
mkdir ..\data
docker compose --env-file ..\.env up -d --force-recreate cyberverse-server
```

