# Sentinel · Guard-AGENT

![go](https://img.shields.io/badge/go-1.24-00ADD8)
![license](https://img.shields.io/badge/license-MIT-green)
![status](https://img.shields.io/badge/status-alpha-orange)

> AI 时代的**安全隔离执行层**。把端侧模型（LFM 2.5）封装成标准化 CLI，
> 解决云端大模型处理私有基础设施（K8s / DB / Cloud API）时
> 「高能力」与「高隐私」无法兼得的矛盾。

`guard run "<自然语言任务>"` —— 本地模型把模糊意图翻译成具体指令，
经过安全围栏校验，再由你确认后执行。**敏感数据在物理意义上不出域。**

---

## 三层过滤架构

```
            guard run "诊断 default 里未就绪的 pod"
                          │
        ┌─────────────────▼──────────────────┐
        │  ① 意图接口层 (Intent Bridge)        │   暴露自然语言入口
        └─────────────────┬──────────────────┘
                          │
        ┌─────────────────▼──────────────────┐
        │  ② 端侧推理芯 (LFM Engine)           │   本地推理，数据不出域
        │   • Local RAG：读取 .kube/.ssh 配置  │
        │   • 意图对齐：意图 → shell/kubectl    │
        │   • Provider：ollama│llamacpp│mlx│mock│
        └─────────────────┬──────────────────┘
                          │  Plan (JSON actions)
        ┌─────────────────▼──────────────────┐
        │  ③ 安全执行围栏 (Policy Guard)        │   allow / confirm / block
        │   • 正则 + 语义拦截（drop / --all …） │
        │   • Human-in-the-loop 确认           │
        └─────────────────┬──────────────────┘
                          │
                     执行 / 拒绝 / 降级
```

设计上**推理后端与编排层解耦**：所有模型后端都走 OpenAI 兼容协议，
换后端只是改配置而非改代码（对应立项书风险评估里的「标准化协议层解耦」）。

---

## 快速开始

```bash
# 构建
go build -o bin/guard ./cmd/guard

# 1) 无需任何模型，用 mock 后端体验完整流程
./bin/guard run --provider mock "诊断 default 命名空间里未就绪的 pod"

# 2) 单独测试安全围栏
./bin/guard policy check "kubectl delete pods --all"   # -> BLOCK
./bin/guard policy check "kubectl get pods"            # -> ALLOW

# 3) 查看本地上下文（不含任何密钥）
./bin/guard context

# 4) 列出技能包
./bin/guard skills
```

默认是 **plan 模式**（只打印计划、不执行）。确认无误后加 `--execute` 才会真正运行，
且 `block` 级指令永远不会被执行、`confirm` 级指令会逐条要求确认。

### 接入真实模型（推荐 Ollama）

```bash
# 拉起一个 OpenAI 兼容的本地推理服务，例如 Ollama
ollama serve
ollama pull <你的-lfm2.5-tag>

export SENTINEL_PROVIDER=ollama
export SENTINEL_MODEL=<你的-lfm2.5-tag>
./bin/guard run "查看 payment 服务最近的日志"
```

> 模型权重不随仓库分发，需自行准备。`*.gguf` / `*.safetensors` / `models/` 已被 gitignore。

---

## 推理后端

所有后端统一走 OpenAI 兼容的 `/v1/chat/completions`，通过 `--provider` 或 `SENTINEL_PROVIDER` 切换。

| provider   | 跨平台 | 说明                                                | 默认 endpoint                 |
|------------|--------|-----------------------------------------------------|-------------------------------|
| `ollama`   | 全平台 | **推荐默认**：自带模型管理、安装简单                 | `http://localhost:11434/v1`   |
| `llamacpp` | 全平台 | 单二进制、无守护进程、可审计（GGUF）                | `http://localhost:8080/v1`    |
| `mlx`      | 仅 Mac | Apple Silicon 上性能最佳                            | `http://localhost:8080/v1`    |
| `mock`     | 全平台 | 无需模型的离线演示后端（CI / 首次体验）             | —                             |

为什么默认选 Ollama、以及 llamafile / ExecuTorch / Apple Foundation Models 等备选，
见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

---

## 安全模型

- **Policy Guard**：每条指令在执行前都被分级为 `allow` / `confirm` / `block`。
  未命中任何规则的指令默认 `confirm`——未知动作永远需要人确认。
- **Human-in-the-loop**：默认 plan 模式；`--execute` 下 `confirm` 逐条确认，`block` 一律拒绝。
- **意图降级**：本地模型无法处理时，**绝不**把上下文发往云端，而是提示用户细化或扩展技能包。
- **本地 RAG 不外泄**：只读取 `current-context` 等非密钥标识注入提示词，不读取凭证内容。

内置拦截规则（节选）：`rm -rf /`、`kubectl delete --all`、`delete namespace`、
`drop/truncate table`、无 `WHERE` 的 `DELETE`、读取 `id_rsa` / `.kube/config` 等。

---

## Roadmap

- **第一阶段 · MVP（当前）**：意图桥 + 端侧推理（OpenAI 兼容协议）+ K8s 技能包 + Policy Guard。
- **第二阶段 · 技能生态**：Database（MySQL/PG）、Cloud CLI（AWS/Aliyun）、Git；完善「意图降级」。
- **第三阶段 · 企业级合规**：SSO 接入、审计日志上报（仅上报操作类型不报数据）、离线模式。

## 风险与对策

| 风险           | 对策                                                       |
|----------------|------------------------------------------------------------|
| 模型幻觉       | 强制 Human-in-the-loop；执行前本地校验（如 `--dry-run`）   |
| 模型升级成本   | 标准化协议层解耦，后端可热插拔                              |

---

## 开发

```bash
make build   # 构建到 bin/guard
make test    # go test ./...
make vet     # go vet ./...
make run     # mock 后端跑一个示例任务
```

无第三方运行时依赖（仅标准库）——对一个安全工具而言，更小的供应链攻击面是刻意的设计取舍。

## License

MIT — 见 [LICENSE](LICENSE)。

> Alpha 阶段，接口与规则仍可能变动；请勿在未审阅 plan 的情况下对生产环境使用 `--execute`。
