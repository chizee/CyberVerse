<h1 align="center">CyberVerse</h1>
<p align="center"><em>CyberVerse は、オープンソースの<strong>リアルタイム音声・映像 Agent プラットフォーム</strong>です。WebRTC、ペルソナ記憶、ツール、RAG、任意のデジタルヒューマン映像機能を基盤に、音声インタラクションを中心とした AI Agent の構築を支援します。</em></p>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a> · <a href="README.ja.md"><strong>日本語</strong></a> · <a href="README.ko.md">한국어</a>
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
### たった一枚の写真から、息づくデジタルヒューマンへ。

> あなたを本当に見て、聞いて、リアルタイムで話しかけてくれる、自分だけの J.A.R.V.I.S. のような AI を夢見たことはありませんか？
>
> もう会えない大切な人に再び会い、その声を聞き、笑顔を見ることができたらどうでしょうか。
>
> あるいは、ずっと命を吹き込みたかったキャラクターがいるかもしれません。
>
> **必要なのはたった一枚の写真。CyberVerse がその存在を動き出させます。**

## デジタルヒューマン Agent とは？

<p align="center">
  <a href="docs/assets/digital-human-agent.jpeg"><img src="docs/assets/digital-human-agent.jpeg" alt="CyberVerse デジタルヒューマン Agent" width="100%"/></a>
</p>

## デモ
<p align="center"><em>以下のキャラクターはデモ例です。CyberVerse に同梱されておらず、商用利用向けには提供されません。</em></p>

<p align="center">
  <a href="docs/assets/character1.png"><img src="docs/assets/character1.png" alt="CyberVerse キャラクター選択ギャラリー" width="100%"/></a>
</p>

<p align="center">
  <a href="docs/assets/character2.png"><img src="docs/assets/character2.png" alt="CyberVerse キャラクター例ギャラリー" width="100%"/></a>
</p>

<div align="center">

| [![](docs/assets/爱丽丝.mov.png)](https://youtu.be/Lk88sew2x4o) | [![](docs/assets/丽娜.mov.png)](https://youtu.be/8jdQ3ThcwgA) |
|:---:|:---:|
| [**Alice — YouTube で見る**](https://youtu.be/Lk88sew2x4o) | [**Lina — YouTube で見る**](https://youtu.be/8jdQ3ThcwgA) |

| [![](docs/assets/小龙女.mov.png)](https://youtu.be/WjEHUYZx5Gs) |
|:---:|
| [**Xiaolongnü — YouTube で見る**](https://youtu.be/WjEHUYZx5Gs) |

</div>

## 特長

### リアルタイム音声 Agent

音声は CyberVerse のデフォルトのインタラクション方式であり、低遅延で長時間続けられるリアルタイム会話を想定しています。ユーザーはマイクで Agent と継続的に会話し、モデルの発話中にいつでも割り込み、同じ会話ターンで音声とテキスト入力を組み合わせられます。

各キャラクターには声、ウェルカムメッセージ、人格設定を個別に構成でき、音声クローンにも対応します。会話中のセッション中断と再開をサポートし、`inference.avatar.enabled` を `false` にすると、プラットフォームは純粋な音声モードで動作し、音声ストリームだけを配信します。ローカル Avatar GPU は不要で、コアの音声体験は変わりません。

### WebRTC による音声・映像

セッション経路は WebRTC 上に構築され、デプロイ環境に応じて直接 P2P（組み込み TURN / NAT トラバーサル）または LiveKit SFU モードを選択できます。低遅延と複雑なネットワーク環境での接続性を両立します。

standard モードと対応する omni セッションでは、Agent がユーザーのカメラ映像や画面共有フレームを視覚入力として受け取ることもできます。純粋なテキスト文脈に限定されず、「聞ける、見られる」対面型のインタラクションを実現します。

### PersonaAgent + SubAgent タスク

CyberVerse は multi-agent アーキテクチャを採用しています。PersonaAgent は常に前面にいて、ユーザーとの滑らかな会話、割り込みへの素早い応答、文脈切り替えを担当します。検索、調査、資料整理、要約、HTML レポート生成などの時間がかかる作業は、バックグラウンド SubAgent が非同期で実行します。

これにより複雑なタスクが音声ターンを遅くしません。ユーザーは話し続けたり、追加で質問したり、方向性を調整したりでき、SubAgent の完了後に結果が前面の会話へ返されます。

### キャラクター記憶と RAG

各キャラクターの会話履歴はローカルディスクに永続化され、会話へ戻ると自動的に読み込まれるため、セッションをまたいだ連続性を保てます。キャラクター用の知識ベース、文書、人物の経歴素材も取り込めます。システムはそれらをインデックス化し、検索拡張生成に利用することで、回答をキャラクターの背景や設定により近づけます。

### 任意のデジタルヒューマン映像

GPU リソースがあり Agent を「見える」存在にしたい場合は、avatar inference を有効にします。1 枚のキャラクター参照画像だけで、FlashHead や LiveAct などの設定可能なバックエンドを通じて、リアルタイム表情アニメーション、リップシンク、キャッシュ済み待機動画の再生を駆動できます。GPU がない場合や、まだ映像が不要な場合は、この機能を無効にすれば純粋な音声 Agent に戻せます。同じキャラクターとペルソナ設定はそのまま使えます。

### プラグインベースのスタック

頭脳、声、聴覚、ツール、記憶、顔はすべて差し替え可能なモジュールです。実行時の挙動は引き続き `config/cyberverse.yaml` に置き、omni model、LLM、TTS、ASR、Embedding の provider 定義は内蔵の `infra/config/*_models/` ディレクトリから自動的に読み込まれます。必要に応じて `config/*_models/` にローカル上書きファイルを置くこともできます。Web UI の **`/settings`** で各ベンダーの API Key とサービスエンドポイントを設定し、用途に応じてプロバイダーやモデル構成を自由に切り替えられます。

## クイックスタート

### 前提条件

- Node 18+
- Go 1.25（必須: `protoc-gen-go`, `protoc-gen-go-grpc`）
- Conda
- Python 3.10+
- FFmpeg
- libopus-dev、libopusfile-dev、libsoxr-dev，pkg-config

> 純粋な音声セッションでは、ローカルの Avatar GPU は不要です。実行コストは、設定したリアルタイム音声 / omni / LLM / TTS / ASR プロバイダーに依存します。

確認には次を実行します:

```bash
node --version
go version
protoc --version
ffmpeg -version
conda --version
```

### ステップ 1: クローンする

```bash
git clone https://github.com/dsd2077/CyberVerse.git
cd CyberVerse
```

### ステップ 2: Python 環境を作成する

```bash
conda create -n cyberverse python=3.10
conda activate cyberverse
```

### ステップ 3: 環境変数を設定する

```bash
cp -r infra/config config
```

`config/env` を編集し、対応する API Key を入力します。

Alibaba Cloud Qwen シリーズモデル:

```env
DASHSCOPE_API_KEY=your_dashscope_api_key
```

または Volcengine Doubao シリーズモデル:

```env
DOUBAO_ACCESS_TOKEN=your_doubao_access_token
DOUBAO_APP_ID=your_doubao_app_id
```

Doubao Voice: [Volcengine クイックスタート](https://www.volcengine.com/docs/6561/2119699?lang=zh)に従って **App ID** / **API Key** を取得し、`DOUBAO_APP_ID` / `DOUBAO_ACCESS_TOKEN` に設定します。

スタック起動後は、API キーやサービスエンドポイントを `config/env` だけでなく Web UI の **`/settings`** から変更できます。

omni、LLM、Embedding、TTS、ASR のモデル定義は `infra/config/*_models/` から自動検出されます。ローカルで上書きしたい場合だけ、同名のモデルファイルを `config/*_models/` に置いてください。

### ステップ 4: ローカル設定を作成して voice-only モードを有効にする

`config/cyberverse.yaml` を編集します。

```yaml
inference:
  avatar:
    enabled: false
```

`enabled: false` の場合、CyberVerse は純粋な音声 Agent アシスタントとして動作します。

### ステップ 5: プロジェクト依存関係をインストールする

```bash
make setup
```

これにより、基本の editable package（`[dev,inference]`）のインストール、gRPC stubs の生成、フロントエンド依存関係のインストールが行われます。

デフォルト設定で使う音声 Agent extras をインストールします。

```bash
# すべての optional グループを一括でインストール
pip install -e ".[all]"
```

### ステップ 6: サービスを起動する（3 つのターミナル）

**ターミナル 1** — Python 推論サーバー:

```bash
conda activate cyberverse
make inference
```

**ターミナル 2** — Go API サーバー:

```bash
make server
```

**ターミナル 3** — フロントエンド:

```bash
make frontend
```

### ステップ 7: 確認する

```bash
# API ヘルスを確認
curl -s http://localhost:8080/api/v1/health
```

ブラウザで http://localhost:5173 を開いてください。

## 任意: 完全なデジタルヒューマン映像

FlashHead または LiveAct でリアルタイム Avatar 映像を駆動したい場合は、以下の手順を実行してください。

### 追加要件

- CUDA 12.8+ に対応した GPU
- PyTorch 2.8（CUDA 12.8）
- `libvpx` を含む FFmpeg（動画エンコード用）
- Avatar モデル重み

PyTorch（CUDA 12.8）をインストールします。

```bash
pip3 install torch==2.8.0 torchvision==0.23.0 torchaudio==2.8.0 --index-url https://download.pytorch.org/whl/cu128
```

LiveAct を使う場合は vllm をインストールします。

```bash
pip install vllm==0.11.0
```

### モデル重みをダウンロードする

CyberVerse は現在 **FlashHead** と **LiveAct** に対応しています。必要なものだけダウンロードしてください。今後もさらにモデルを追加していきます。

```bash
pip install "huggingface_hub[cli]"
```

#### FlashHead（SoulX-FlashHead）

| モデルコンポーネント | 説明 | リンク |
| :--- | :--- | :--- |
| `SoulX-FlashHead-1_3B` | 1.3B FlashHead 重み | [Hugging Face](https://huggingface.co/Soul-AILab/SoulX-FlashHead-1_3B), [ModelScope](https://modelscope.cn/models/Soul-AILab/SoulX-FlashHead-1_3B) |
| `wav2vec2-base-960h` | 音声特徴抽出器 | [Hugging Face](https://huggingface.co/facebook/wav2vec2-base-960h), [ModelScope](https://modelscope.cn/models/facebook/wav2vec2-base-960h) |

```bash
# 中国本土から利用する場合は、先にミラーを設定できます:
# export HF_ENDPOINT=https://hf-mirror.com

hf download Soul-AILab/SoulX-FlashHead-1_3B \
  --local-dir ./checkpoints/SoulX-FlashHead-1_3B

hf download facebook/wav2vec2-base-960h \
  --local-dir ./checkpoints/wav2vec2-base-960h
```

#### LiveAct（SoulX-LiveAct）

| モデル名 | ダウンロード |
|-----------|----------|
| SoulX-LiveAct | [Hugging Face](https://huggingface.co/Soul-AILab/LiveAct), [ModelScope](https://modelscope.cn/models/Soul-AILab/LiveAct) |
| chinese-wav2vec2-base | [Hugging Face](https://huggingface.co/TencentGameMate/chinese-wav2vec2-base), [ModelScope](https://modelscope.cn/models/TencentGameMate/chinese-wav2vec2-base) |

```bash
hf download Soul-AILab/LiveAct \
  --local-dir ./checkpoints/LiveAct

hf download TencentGameMate/chinese-wav2vec2-base \
  --local-dir ./checkpoints/chinese-wav2vec2-base
```

### Avatar Inference を設定する

`config/cyberverse.yaml` で `enabled` を `true` にします。モデル固有の設定は
`config/avatar_models/` 配下にモデルごとの YAML として置き、そこにローカル checkpoint
のパスを書きます。

```yaml
inference:
  avatar:
    enabled: true
    default: "flash_head"
    idle_strategy: "silent_inference"
    runtime:
      cuda_visible_devices: 0      # 共有 GPU ID。マルチ GPU の場合は 0,1 など
      world_size: 1                # 共有 GPU 数。デュアル GPU なら 2
    model_config_dir: "avatar_models"
```

次に `config/avatar_models/flash_head.yaml` や `config/avatar_models/live_act.yaml` を編集します。
これらのモデルパラメータは Web UI からも調整でき、対応するモデル設定ファイルへ書き戻されます。

### LiveAct FP4 GEMM（任意）

FP4 アクセラレーションには [LightX2V](https://github.com/ModelTC/LightX2V) から `lightx2v_kernel` をビルド・インストールする必要があります。ビルド環境では **PyTorch 2.7+** と CUTLASS のソースを用意してください。

#### 準備

```bash
pip install scikit_build_core uv
```

#### whl のビルド

```bash
git clone https://github.com/NVIDIA/cutlass.git
git clone https://github.com/ModelTC/LightX2V.git
cd LightX2V/lightx2v_kernel
# /path/to/cutlass をローカルの cutlass クローンの絶対パスに置き換えてください。
MAX_JOBS=$(nproc) && CMAKE_BUILD_PARALLEL_LEVEL=$(nproc) \
uv build --wheel \
    -Cbuild-dir=build . \
    -Ccmake.define.CUTLASS_PATH=/path/to/cutlass \
    --verbose \
    --color=always \
    --no-build-isolation
```

#### whl のインストール

```bash
pip install dist/*.whl --force-reinstall --no-deps
```

#### CyberVerse で有効化

`config/avatar_models/live_act.yaml`（または Web UI）の `live_act` で次を設定します：

```yaml
fp8_gemm: false
fp4_gemm: true
```

これらのフラグを変更したあと、推論サービスを再起動してください。

### SageAttention と FlashAttention（任意）

```bash
# SageAttention（ソースからビルド）
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

### Avatar ハードウェアベンチマーク

リアルタイムのデジタルヒューマン映像には GPU アクセラレーションが必要です。以下は FlashHead と LiveAct アバターモデルのベンチマークです。

| モデル | 品質 | GPU | 枚数 | 解像度 | FPS | リアルタイム可？ |
|-------|---------|-----|-------|------------|-----|------------|
| FlashHead 1.3B | Pro | RTX 5090 | 2 | 512×512 | 25+ | ✅ はい |
| FlashHead 1.3B | Pro | RTX 5090 | 1 | 464x464 | 20 | ✅ はい |
| FlashHead 1.3B | Pro | RTX PRO 6000 | 1 | 512×512 | 20 | ✅ はい |
| FlashHead 1.3B | Pro | RTX 4090 | 1 | 512×512 | ~10.8 | ❌ いいえ |
| FlashHead 1.3B | Lite | RTX 4090 | 1 | 512×512 | 25+ | ✅ はい |
| LiveAct 18B | — | RTX PRO 6000 | 2 | 320×480 | 20 | ✅ はい |
| LiveAct 18B | — | RTX PRO 6000 | 1 | 256×417 | 20 | ✅ はい |

> **Pro** は画質優先、**Lite** は速度優先です。表は代表的な **画質と計算資源のバランス** の例です。余裕があれば画質を上げられ、不足なら解像度や **Pro** / **Lite** など画質側の設定を下げてリアルタイム性を確保してください。

avatar inference が有効な場合、`make inference` は `config/cyberverse.yaml` の `inference.avatar.default` を読み取り、現在の推論プロセスではその 1 つのアバターモデルだけを初期化します。次のログが出るまで待ちます。

- `Active avatar model initialized: <model_name>`
- `CyberVerse Inference Server started on port 50051`

## よくある質問 — 自己チェック（QA）

アバター映像が**カクつく、止まる、音声より遅れる**ときは、まず推論が再生に追いついているかを確認してください。

### 推論ログで RTP を確認する

**RTP**（リアルタイム性能係数）は、チャンクの生成にかかった時間と、そのチャンクを設定 FPS で再生するのに必要な時間の比です。

```text
RTP = elapsed / (frames / fps)
```

| RTP | 意味 |
|-----|------|
| **&lt; 1** | 生成が再生より速い — リアルタイム配信に余裕あり |
| **= 1** | ちょうどリアルタイム |
| **&gt; 1** | 生成が再生より遅い — **産出が消費に追いつかない**ため、遅延やカクつきが起きやすい |

キャラクターが話している間、推論ターミナル（`make inference`）のログで **LiveAct** または **FlashHead** の chunk 行を確認します。

**LiveAct の例（RTP &gt; 1 — リアルタイム不可）：**

```text
INFO:inference.plugins.avatar.live_act_plugin:LiveAct chunk: idx=2 frames=32 320x480 fps=20 iter=2 elapsed=1.870s is_final=False
```

- この chunk の再生時間：`32 / 20 = 1.6` 秒  
- RTP：`1.870 / 1.6 ≈ 1.17`（**&gt; 1** — この GPU では 320×480 @ 20 fps に追いつかない）

**FlashHead** も同様に、`elapsed` と `num_frames`、`fps` から計算します。

```text
INFO:...FlashHead video chunk generated: chunk_index=1 num_frames=33 512x512 fps=20 ... elapsed=2.100s
```

この例では RTP = `2.100 / (33/20) ≈ 1.27` で、リアルタイムを超えています。

### RTP &gt; 1 のときの対処

1. **解像度または画質を下げる** — 例：LiveAct の `infer_params.size`、FlashHead の `height` / `width`、または FlashHead を `model_type: "lite"` にする。
2. **計算資源を増やす** — GPU を増やす（`runtime.world_size`、`cuda_visible_devices`）、対応環境では FP8/FP4 GEMM やコンパイル加速を有効化、より高速な GPU を使う。
3. **上のベンチマーク表に合わせる** — [Avatar ハードウェアベンチマーク](#avatar-ハードウェアベンチマーク) で **リアルタイム？** が「はい」の解像度・FPS・GPU の組み合わせを選ぶ。

純粋な音声モード（`inference.avatar.enabled: false`）では Avatar の RTP は関係しません。音声のみでカクつく場合は、ネットワーク/WebRTC や上流の音声遅延を疑い、[リモートアクセスメモ](#リモートアクセスメモ) を参照してください。

## リモートアクセスメモ

`streaming_mode: direct` で組み込み TURN を使う場合、ブラウザはサーバーの `8443/TCP` に到達できる必要があります。ページは開けるのに音声・映像がいつまでも接続されない、またはサーバーログに `ICE connection state: failed` や `publish timeout waiting for connection` が出る場合は、まず手元の端末からサーバーの `8443` ポートに疎通できるか確認してください。

```bash
nc -vz <server-ip> 8443
```

`8443` に到達できない場合、原因はクラウドのセキュリティグループ、ファイアウォール、または NAT 制限であることが一般的です。その場合は、SSH トンネルでローカルの `8443` をサーバーへ転送できます。

```bash
ssh -L 8443:127.0.0.1:8443 user@host -p port
```

トンネル確立後、ブラウザはローカルの `127.0.0.1:8443` 経由でリモート TURN サービスへ接続します。

SSH トンネルではなくブラウザからリモートサーバーへ直接接続したい場合は、`config/cyberverse.yaml` の `pipeline.ice_public_ip` にサーバーのグローバル IP またはドメインを設定してください。SSH トンネルを使う場合は、デフォルト値（`127.0.0.1`）のままで構いません。

## ロードマップ

### 1. **リアルタイム音声・映像 Agent プラットフォーム**

音声ファーストのリアルタイム Agent を、実行・カスタマイズ・埋め込みしやすくします。

- [x] 複数の参照画像、アクティブ画像、固定 / ランダム表示モード、任意の顔切り抜き、タグ、音声フィールド、人格、ウェルカムメッセージ、システムプロンプトを備えたキャラクター CRUD
- [x] WebRTC によるリアルタイム音声セッション。直接 P2P（組み込み TURN）または LiveKit SFU
- [x] `inference.avatar.enabled: false` による純粋な音声セッション
- [x] omni model、LLM、TTS、ASR、Embedding、RAG、avatar をプラグインとして提供し、YAML と UI settings で各ベンダーの API キーを設定可能
- [x] セッション管理：キャラクター単位で会話履歴をディスクに永続化し、会話開始時に読み込み
- [x] 音声クローン：豆包音声の音声クローンに対応
- [x] 音声とテキストのハイブリッド入力に対応
- [x] モデル発話中の音声割り込みとセッションの中断・再開
- [x] standard モードと対応する omni セッションで、ユーザー側カメラ入力と画面共有フレームに対応
- [x] PersonaAgent とバックグラウンド SubAgent タスク実行
- [x] 知識・文書・人物の生平などの素材を取り込み、キャラクターに沿った RAG による回答
- [ ] 開発者向けのサイト埋め込み（Web コンポーネントまたは SDK）、自己ホストしたインスタンスを自サイトへ接続
- [ ] ライブ配信向けの音声・映像ストリーミング

### 2. **リアルタイムデジタルヒューマン通話**

Avatar 用 GPU リソースがある場合、音声 Agent をリアルタイム映像通話に変えます。

- [x] 参照画像から、設定可能な Avatar プラグイン（FlashHead、LiveAct など）でリアルタイムのアバター映像を駆動
- [x] キャラクター存在感のための待機動画キャッシュ再生
- [x] リアルタイム発話セグメントの音声・映像同期
- [ ] 画質、遅延、コストのトレードオフが異なる Avatar バックエンドを追加
- [ ] コンシューマー GPU、ワークステーション GPU、クラウド GPU 向けのより良い Avatar デプロイプロファイル

### 3. **エージェントネットワーク**

複数のエージェントを接続し、相互にコミュニケーションし、協調し、ネットワークを形成できるようにします。

- [ ] agent-to-agent 通信を有効化
- [ ] マルチエージェント協調と委譲を有効化
- [ ] エージェント間の共有メモリと共有知識を有効化
- [ ] 接続されたエージェントのオープンネットワークを構築

## コミュニティ

<p align="center">
  <a href="docs/assets/wechat_group.jpg"><img src="docs/assets/wechat_group.jpg" alt="CyberVerse WeChat グループ QR コード" width="320"/></a>
</p>

<p align="center">QR コードの有効期限が切れた場合は、管理者の WeChat <strong>wx_dsd2077</strong> に追加し、申請時に <strong>CyberVerse</strong> と備考してください。グループへ招待します。</p>

## Star History

<p align="center">
  <a href="https://star-history.com/#dsd2077/CyberVerse&Date">
    <img src="https://api.star-history.com/svg?repos=dsd2077/CyberVerse&type=Date" alt="Star History Chart" width="100%"/>
  </a>
</p>

## ライセンス

GNU General Public License v3.0。詳細は [LICENSE](LICENSE) を参照してください。

## 謝辞

- [SoulX-FlashHead](https://github.com/Soul-AILab/SoulX-FlashHead) — Soul AI Lab によるアバターモデル

- [SoulX-LiveAct](https://github.com/Soul-AILab/SoulX-LiveAct) - Soul AI Lab によるアバターモデル
- [MuseTalk](https://github.com/TMElyralab/MuseTalk) — TME Lyra Lab によるリアルタイムリップシンクモデル
- [Pion](https://github.com/pion/webrtc) — Go の WebRTC 実装
- [Linux.do](https://linux.do/)
