<p align="center">
  <img src="docs/assets/banner.png" width="840" alt="Sentinel-Agent">
</p>

<h1 align="center">Sentinel-Agent</h1>

<p align="center">
  端侧 AI 运维 Agent —— 介于云端大模型与你的私有基础设施之间的<b>安全隔离执行层</b>。
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.24-00ADD8" alt="go">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="license">
  <img src="https://img.shields.io/badge/status-alpha-orange" alt="status">
  <img src="https://img.shields.io/badge/deps-stdlib%20only-blue" alt="deps">
</p>

<p align="center">
  <a href="README.md">English</a> · <b>简体中文</b>
</p>

---

`guard run "<自然语言任务>"` —— 本地模型把模糊意图翻译成具体指令，经过安全围栏校验，再由你确认后执行。
**敏感数据在物理意义上不出域。**

云端大模型很强，但越来越多团队禁止把 K8s 配置、私有代码、数据库凭证贴进去。Sentinel-Agent 就是那个
**合规的出口**：高层推理放在任何你信任的地方，而特权操作交给端侧模型在安全围栏后完成。

## 演示

**CLI** —— 自然语言 → 端侧 LFM2.5 → Policy Guard → 对真实 minikube 集群执行 `kubectl`
（`ImagePullBackOff` 那个坏 pod 是真的）；破坏性命令一律拒绝。

<p align="center">
  <img src="docs/assets/demo-cli.gif" width="760" alt="guard CLI 对接真实 minikube 集群">
</p>

**Sentinel Skill** —— 云端 Agent 通过 `guard skill`（JSON 协议）排查**并修复**一次真实生产故障：`shop`
命名空间里 `payment-api` 正在 CrashLoopBackOff。Agent 读取非密钥上下文、端侧规划、执行只读命令定位根因
（缺少环境变量）、提出修复命令 → **readonly 模式拒绝自动变更、要求人工审批** → 审批后以 `auto` 模式执行修复
→ pod 恢复 Running。全程批量删除仍被拦截。

<p align="center">
  <img src="docs/assets/demo-skill.gif" width="760" alt="Sentinel Skill 排查真实生产故障">
</p>

<sub>复现：<a href="docs/demo-cli.sh">docs/demo-cli.sh</a> · <a href="docs/demo-skill.sh">docs/demo-skill.sh</a>（asciinema + agg 录制）</sub>

## 安装（macOS 优先）

开箱即用：装好小巧的 CLI、装一次推理引擎，然后直接跑——端侧模型首次运行时自动下载。无需 Ollama，无需手动折腾模型。

```bash
# 1. CLI 本体（很小，不含模型）
go install github.com/xiaokhkh/sentinel-agent/cmd/guard@latest

# 2. 本地推理引擎，装一次
brew install llama.cpp

# 3. 直接跑——首次运行自动拉取一个小的 LFM2.5 模型（约 0.8 GB）并在本地起服务
guard run "诊断 default 命名空间里未就绪的 pod"
```

`guard` 会自己下载 GGUF——**走代理**（Go HTTP 尊重 `HTTPS_PROXY`，不像 llama.cpp 的 `-hf` 会无视代理）——
存到 `~/.sentinel/models/`，再以 `llama-server -m ...` 绑定 `127.0.0.1` 启动。在墙内可设 `HF_ENDPOINT` 用镜像，
或直接靠你的代理。量化档用 `SENTINEL_QUANT` 选（默认 `Q4_K_M`）。管理命令：

```bash
guard model    # 查看 模型 / 引擎 / 端点 状态
guard serve    # 前台运行引擎
guard stop     # 停止后台引擎
```

还没装引擎、或只想先体验流程？`guard run --provider mock "..."` 用内置 mock 后端离线跑通整条管线。

## 用户如何使用

两种模式、同一内核，按场景选用。

### 模式一 —— CLI（独立，完全本地）

在终端里直接对话。适合交互式运维、脚本、断网机器。

```bash
go build -o bin/guard ./cmd/guard

# 无需任何模型，mock 后端即可端到端体验
./bin/guard run --provider mock "诊断 default 命名空间里未就绪的 pod"

# 单独把一条指令丢给安全围栏判定
./bin/guard policy check "kubectl delete pods --all"   # -> BLOCK

# 默认 readonly（跑只读、遇写操作询问）；用 --mode 提升自治级别（Claude Code / Codex 式分级）
./bin/guard run --mode readonly "查看 payment 服务日志"   # 跑只读，遇写操作询问
./bin/guard run --mode auto     "重启 nginx deployment"   # 读写都跑
```

权限分级（`--mode`）与 Policy Guard 判定组合决定行为：

| 判定 \ 模式         | `readonly` | `auto` | `full` |
|---------------------|:----------:|:------:|:------:|
| allow（只读）       | 执行       | 执行   | 执行   |
| confirm（变更）     | 询问       | 执行   | 执行   |
| block（危险）       | 拒绝       | 拒绝   | 执行 ⚠ |

### 模式二 —— Sentinel Skill（云端 Agent + 端侧安全执行）

把 Sentinel Skill 安装到云端 Agent（Claude、Codex、Cursor…）里。Skill 给 Agent 一个安全的本地运维能力：
云端做高层规划，Sentinel 在本机执行具体步骤、对输出**脱敏**后回传，云端据此推理而永远看不到原始密钥。
Skill 通过本机 `guard skill` CLI JSON 接口与本地运行时通信。

```
 云端大模型(规划者) — Claude Desktop / Cursor / Codex
        │  guard skill plan / exec("kubectl logs ...")   ← 只有意图离开本机
        ▼
   Sentinel 本地运行时   (本机 = 执行者 + 脱敏器)
   ├─ LFM Engine   → 端侧模型规划 / 细化
   ├─ Local RAG    → 读取 kube/ssh 上下文      (绝不外传)
   ├─ Policy Guard → allow / confirm / block  ×  mode (plan/readonly/auto/full)
   └─ Redactor     → 抹去 key / token / 凭证 / 邮箱
        │  仅回传脱敏后的结果                            → 返回云端规划者
        ▼
   云端规划下一步  ──▶  循环
```

这就是**合规的出口**：强模型留在云端，特权操作与原始数据留在本机，只有脱敏后的观测结果跨越边界。

把 Skill 接到本地运行时：让 Agent 所在机器能执行 `guard`。

```bash
go install github.com/xiaokhkh/sentinel-agent/cmd/guard@latest
guard skill context
```

Skill 包：[docs/skills/sentinel-agent/SKILL.md](docs/skills/sentinel-agent/SKILL.md)。
它是 Agent 侧契约：什么时候看本地上下文、什么时候规划、什么时候执行、什么时候停下来等审批。

Skill 使用的 CLI 调用：`guard skill context`、`guard skill plan`、`guard skill exec`、
`guard skill policy`。Skill 运行时自治级别由 `SENTINEL_MODE` 决定（默认 `readonly`）；
对 `ask` 级（变更类）步骤，Agent 客户端自身的工具调用审批弹窗即人工门。

## 架构与流程

<p align="center">
  <img src="docs/assets/architecture.svg" width="760" alt="Sentinel-Agent 架构与流程">
</p>

云端规划者只下发意图；端侧四层（意图接口层 → 端侧推理芯 → 安全围栏 → Redactor）在本地规划并执行，
只有**脱敏后**的结果回环。推理后端与编排层解耦：所有后端走 OpenAI 兼容协议，换后端是改配置而非改代码。
详见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) 与 [Tool Call 协议](docs/tool-call-protocol.md)。

## 推理后端

通过 `--provider` 或 `SENTINEL_PROVIDER` 切换，统一走 OpenAI 兼容 `/v1/chat/completions`。

| provider   | 跨平台 | 说明                                   | 默认 endpoint                 |
|------------|--------|----------------------------------------|-------------------------------|
| `ollama`   | 全平台 | **推荐默认**；自带模型管理             | `http://localhost:11434/v1`   |
| `llamacpp` | 全平台 | 单二进制、无守护进程、可审计（GGUF）   | `http://localhost:8080/v1`    |
| `mlx`      | 仅 Mac | Apple Silicon 上性能最佳               | `http://localhost:8080/v1`    |
| `mock`     | 全平台 | 无需模型的离线演示后端（CI / 首次体验）| —                             |

> 模型权重不随仓库分发（`*.gguf` / `*.safetensors` / `models/` 已 gitignore）。
> 把 `SENTINEL_MODEL` 指向你自己的 LFM 2.5 构建。

## 安全模型

- **端侧安全边界**：云端 Agent 可以做规划，但敏感数据不得持久化、传输或静默升级到云端。
  kubeconfig 原文、SSH key、云厂商 token、数据库凭证、私有源码、未脱敏生产输出都必须留在本机。
  详见 [docs/on-device-security.md](docs/on-device-security.md)。
- **Policy Guard**：每条指令执行前都被分级 `allow` / `confirm` / `block`；未命中任何规则默认 `confirm`——未知动作永远需要人确认。
- **权限分级**：Claude Code / Codex 式的 `--mode`（`readonly`/`auto`/`full`）与判定组合决定 执行/询问/拒绝。CLI 与 Skill 默认均为 `readonly`：只读执行、变更询问、危险拒绝。
- **脱敏（desensitization）**：任何可能离开本机的执行输出先经脱敏——私钥、JWT、云厂商 key、kubeconfig 密钥、URL 里的凭证、邮箱、长 base64 块统统抹去。在云端规划 loop 下，隐私承诺是**"只出脱敏数据"**，而 Redactor 就是这条保证的命门。
- **端侧语义安全层**（可选，`SENTINEL_SEMANTIC=1`）：在正则基线**之上**叠一层本地 LLM——抓正则漏掉的密钥/PII（如裸 `ghp_…`/`sk_live_…` token），并对**未命中规则的破坏性命令**判定风险（如 `terraform destroy` → 拦截）。这是**只能在端侧做**的安全活：要"看着原始数据决定哪里该藏"，本就不能交给云端模型。只用本地模型、fail-safe（出错退回正则）、且只加严不放松。
- **意图降级**：本地模型给不出计划时，**绝不**把原始任务静默升级到云端，而是把降级显式抛给你/客户端。
- **本地 RAG 不外泄**：只把 `current-context` 等非密钥标识注入提示词，凭证与文件内容从不读取或外传。

## 结构化记忆

`guard` 在 `~/.sentinel/config.json` 里记住「如何访问你的系统」——**只存路径、引用与事实，绝不存密钥**：

```bash
guard config set kubernetes.kubeconfig ~/.kube/config
guard config set kubernetes.namespace payments
guard remember "payment service runs in namespace payments"
guard config        # 查看
guard memory        # 列出记住的事实
```

当任务需要的访问信息还没有（例如没有 kubeconfig）时，**系统提示词**会让模型在对话里向你索取，而不是瞎猜——
答案会被保存下来供下次使用：

```
$ guard run "check my k8s pods"
Which kubeconfig should I use?
> ~/.kube/config
saved to ~/.sentinel/config.json (kubernetes.kubeconfig)
...
```

记住的上下文（kube context/namespace + facts，均非密钥）会注入模型提示词并用于构造命令。

## Roadmap

- **第一阶段 · MVP（当前）**：意图桥、端侧推理（OpenAI 兼容）、K8s 技能包、Policy Guard、Sentinel Skill。
- **第二阶段 · 技能生态**：Database（MySQL/PG）、Cloud CLI（AWS/Aliyun）、Git；完善意图降级。
- **第三阶段 · 企业级合规**：SSO、审计日志（仅上报操作类型不报数据）、离线模式。

## 开发

```bash
make build   # 构建到 bin/guard
make test    # go test ./...
make vet     # go vet ./...
make run     # mock 后端跑一个示例任务
```

无第三方运行时依赖（仅标准库）—— 对安全工具而言，更小的供应链攻击面是刻意的取舍。

## License

MIT —— 见 [LICENSE](LICENSE)。

> Alpha 阶段，接口与规则仍可能变动；请勿在未审阅 plan 的情况下对生产环境使用 `--execute`。
