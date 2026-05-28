<p align="center">
  <img src="docs/assets/logo.svg" width="180" alt="Sentinel-Agent logo">
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

# 默认 plan 模式；加 --execute 才真正执行（且逐条确认）
./bin/guard run --execute "重启 nginx deployment"
```

### 模式二 —— MCP 服务（云端编排 + 端侧安全执行）

运行 `guard mcp`，在任意 MCP 客户端（Claude Desktop、Cursor、Codex…）里注册它。
云端模型只下发**意图**；由**端侧**模型规划、Policy Guard 审查，最终只把审查后的计划回传。
你的 kube/ssh 配置与密钥从不外传。

```
 云端大模型 (Claude Desktop / Cursor / Codex)
        │  run_task("诊断 ...")            ← 只有意图离开本机
        ▼
   guard mcp   (本机)
   ├─ LFM Engine   → 端侧模型生成计划
   ├─ Local RAG    → 读取 kube/ssh 上下文   (绝不外传)
   └─ Policy Guard → allow / confirm / block
        │  仅回传审查后的计划              → 返回云端客户端
        ▼
   你用 `guard run --execute` 执行          (Human-in-the-loop)
```

注册方式（Claude Desktop / 通用 MCP 客户端的 `mcpServers` 配置）：

```jsonc
{
  "mcpServers": {
    "sentinel-agent": {
      "command": "guard",
      "args": ["mcp"],
      "env": { "SENTINEL_PROVIDER": "ollama", "SENTINEL_MODEL": "lfm2.5" }
    }
  }
}
```

或用 Codex：`codex mcp add sentinel-agent -- guard mcp`

通过 MCP 暴露的工具：`run_task`、`policy_check`、`local_context`、`list_skills`。
其中 `run_task` 刻意设计为**只规划不执行** —— 执行始终保留给 Human-in-the-loop 的 CLI。

## 三层过滤架构

```
        guard run "诊断 default 里未就绪的 pod"
                          │
   ① 意图接口层  ─────▼─────  自然语言入口 (CLI / MCP)
   ② 端侧推理芯  ─────▼─────  本地推理，数据不出域
        • Local RAG：读取 ~/.kube、~/.ssh 作背景（仅非密钥信息）
        • 意图对齐：意图 -> shell / kubectl
        • Provider：ollama | llamacpp | mlx | mock（OpenAI 兼容）
   ③ 安全执行围栏 ─────▼─────  allow / confirm / block
        • 正则 + 语义拦截（drop / --all / rm -rf …）
        • Human-in-the-loop 确认
                          │
                    执行 / 拒绝 / 降级
```

推理后端与编排层解耦：所有后端走 OpenAI 兼容协议，换后端是改配置而非改代码。
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

- **Policy Guard**：每条指令执行前都被分级 `allow` / `confirm` / `block`；未命中任何规则默认 `confirm`——未知动作永远需要人确认。
- **Human-in-the-loop**：默认 plan 模式；`--execute` 下 `confirm` 逐条确认，`block` 一律拒绝。
- **意图降级**：本地模型处理不了时，**绝不**把上下文发往云端，而是提示你细化或扩展技能包。
- **本地 RAG 不外泄**：只把 `current-context` 等非密钥标识注入提示词，凭证与文件内容从不读取或外传。

## Roadmap

- **第一阶段 · MVP（当前）**：意图桥、端侧推理（OpenAI 兼容）、K8s 技能包、Policy Guard、MCP 服务。
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
