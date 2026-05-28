# 架构说明

Sentinel 采用「三层过滤模型」，核心原则：**敏感数据在物理意义上不出域**。

## 三层

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
task ──▶ LoadLocalContext() ──▶ Inferencer.Plan() ──▶ Plan{actions}
                                                          │
                              每条 action ──▶ Guard.Evaluate() ──▶ Verdict
                                                          │
                          plan 模式打印 / execute 模式按 verdict 执行或拒绝
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
internal/mcp         MCP stdio 服务（云端编排 + 端侧安全执行）
internal/skills      技能包注册表
internal/skills/k8s  Kubernetes 技能包
internal/config      env + 默认值配置
```

## 两种使用形态

- **CLI**：人在终端直接 `guard run`，完全本地、含 Human-in-the-loop 执行。
- **MCP 服务**（`guard mcp`）：以 stdio JSON-RPC 暴露 `run_task` / `execute_step` /
  `policy_check` / `local_context` / `list_skills`。端侧模型作为云端模型的 skill/tool：
  云端规划，端侧执行具体步骤并对输出脱敏后回传，形成「云端规划 → 端侧执行+脱敏 → 再次交互」的 loop。

## 权限分级（readonly / auto / full）

借鉴 Claude Code 的权限模式与 Codex 的 sandbox/approval。执行结果 = Policy Guard 判定 × 模式：

| 判定 \ 模式      | readonly | auto | full |
|------------------|----------|------|------|
| allow（只读）    | 执行     | 执行 | 执行 |
| confirm（变更）  | 询问     | 执行 | 执行 |
| block（危险）    | 拒绝     | 拒绝 | 执行 |

- CLI 与 MCP 默认均为 `readonly`。
- CLI 下「询问」= 终端 y/N；MCP 下「询问」= 返回 `approval_required`，由 MCP 客户端的工具审批弹窗做人工门。
- `execute_step` 真正执行时，输出先过 `internal/redact` 脱敏再回传——云端 loop 下「只出脱敏数据」。
