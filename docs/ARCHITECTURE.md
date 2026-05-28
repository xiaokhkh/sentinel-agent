# 架构说明

Sentinel 采用「三层过滤模型」，核心原则：**敏感数据在物理意义上不出域**。

## 三层 + 端侧安全边界

### ① 意图接口层 (Intent Bridge) — `internal/cli`
暴露 `guard run "<task>"` 等自然语言入口，负责参数解析、子命令分发与结果汇总。

### ② 端侧推理芯 (LFM Engine) — `internal/engine`
- **Local RAG** (`rag.go`)：读取本地 `~/.kube/config`、`~/.ssh/config` 作为推理背景。
  只提取 `current-context` 等**非密钥标识**与「文件是否存在」，绝不读取凭证内容，也绝不外传。
- **意图对齐**：把自然语言翻译为具体的 `shell` / `kubectl` 指令，输出结构化 `Plan`。
- **Provider 抽象** (`openai.go` + `provider_*.go`)：所有模型后端实现同一个 `Inferencer` 接口，
  统一走 OpenAI 兼容协议。换后端是改配置，不是改代码。

### ③ 安全执行围栏 (Policy Guard) — `internal/policy` + `internal/executor`
- **拦截规则** (`rules.go`)：有序正则规则，分级 `allow` / `confirm` / `block`，首条命中生效，
  最危险、最具体的规则排在最前。未命中默认 `confirm`。
- **Human-in-the-loop** (`executor.go`)：默认 plan 模式只打印不执行；`--execute` 下
  `confirm` 逐条确认、`block` 一律拒绝。

### ④ 端侧安全边界 (On-device Security Boundary) — `internal/redact` + runtime policy

端侧安全不是一个可选插件，而是横切整个执行链路的边界：

- 云端 Agent 只允许拿到高层意图、非密钥标识、策略判定和脱敏后的观测结果。
- kubeconfig、SSH key、云厂商 token、数据库凭证、原始生产日志、私有文件内容都必须留在本机。
- 本地模型无法产出计划时，Sentinel 只能显式降级并请求人类输入，不能把原始上下文静默升级给云端模型。
- 任何可能跨越边界的 stdout/stderr、日志和结构化结果，都必须先经过 `internal/redact`。
- 脱敏器不可用、敏感性不确定、或输出分类失败时，默认不回传云端。

完整数据边界见 [on-device-security.md](on-device-security.md)。

## Provider 选型

为什么默认 **Ollama**：跨平台、自带模型管理、OpenAI 兼容、生态最大、DX 最好。

| 后端     | 角色                                   |
|----------|----------------------------------------|
| ollama   | 默认；即开即用                          |
| llamacpp | 无守护进程、单二进制、可审计           |
| mlx      | Apple Silicon 性能档                    |
| mock     | 无模型的离线演示 / CI                   |

其它可纳入的方向：

- **llamafile** —— 把模型 + 引擎打包成单个跨平台可执行文件，适合「端侧安全工具一键分发」。
- **ExecuTorch** —— 真·移动端 / 嵌入式 AOT 编译，未来上 iOS/Android 时考虑。
- **Apple Foundation Models**（macOS 26）—— 系统自带端侧模型，可作为「未装 LFM 时的免费兜底」。
- **vLLM / SGLang** —— 数据中心吞吐导向，不适合端侧单用户，不纳入。

> LFM 2.5（Liquid AI 的 LFM2 系列）官方提供 GGUF（→ llama.cpp / Ollama）与 MLX 转换版，
> 上述三个 HTTP 后端均可承载；具体版本的最新运行时支持以官方文档为准。

## 数据流

```
cloud/local task ──▶ Intent Bridge ──▶ LoadLocalContext()
                                            │
                                            v
                                     Inferencer.Plan()
                                            │
                                            v
                                      Plan{actions}
                                            │
                         每条 action ──▶ Guard.Evaluate() ──▶ Verdict
                                            │
                                            v
                         plan 模式打印 / execute 模式按 verdict 执行或拒绝
                                            │
                                            v
                              Redactor.Sanitize() ──▶ 脱敏结果才可回云端
```

## 包结构

```
cmd/guard            CLI 入口
internal/cli         子命令分发（意图接口层）
internal/engine      推理核心、RAG、provider 抽象（端侧推理芯）
internal/policy      Policy Guard 规则与判定（安全执行围栏）
internal/permission  权限分级（plan/readonly/auto/full）× 判定 → 执行/询问/拒绝
internal/redact      脱敏器：输出离开本机前抹去密钥/凭证（云端 loop 的隐私命门）
internal/executor    Human-in-the-loop 执行器
internal/llama       本地 llama.cpp 生命周期：检测 llama-server、-hf 自动下载模型、起停
internal/mcp         可选 MCP stdio 适配
internal/skills      技能包注册表
internal/skills/k8s  Kubernetes 技能包
internal/config      env + 默认值配置
```

## 两种使用形态

- **CLI**：人在终端直接 `guard run`，完全本地、含 Human-in-the-loop 执行。
- **Sentinel Skill**：给 Claude/Codex/Cursor 类云端 Agent 一个安全的本地运维能力。
  云端规划，端侧执行具体步骤并对输出脱敏后回传，形成「云端规划 → 端侧执行+脱敏 → 再次交互」的 loop。

Skill 通过本机 `guard skill` CLI JSON 接口通信：`context` / `plan` / `exec` / `policy`。
强制安全约束来自 Sentinel 本地运行时里的 Policy Guard、权限分级与 Redactor；MCP 只是可选适配层。

## 权限分级（readonly / auto / full）

借鉴 Claude Code 的权限模式与 Codex 的 sandbox/approval。执行结果 = Policy Guard 判定 × 模式：

| 判定 \ 模式      | readonly | auto | full |
|------------------|----------|------|------|
| allow（只读）    | 执行     | 执行 | 执行 |
| confirm（变更）  | 询问     | 执行 | 执行 |
| block（危险）    | 拒绝     | 拒绝 | 执行 |

- CLI 与 Skill 默认均为 `readonly`。
- CLI 下「询问」= 终端 y/N；Skill 下「询问」= 返回 `approval_required`，由 Agent 客户端的工具审批弹窗做人工门。
- `guard skill exec` 真正执行时，输出先过 `internal/redact` 脱敏再回传——云端 loop 下「只出脱敏数据」。
