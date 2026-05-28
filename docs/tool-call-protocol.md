# Tool Call 协议规范（草案）

端侧模型与编排层之间的契约。目标是让任意 OpenAI 兼容后端（Ollama / llama.cpp / mlx）
都能在不改代码的前提下接入。

## 请求

标准 OpenAI `chat/completions`，`stream: false`：

```jsonc
POST {base_url}/chat/completions
{
  "model": "<tag>",
  "messages": [
    { "role": "system", "content": "<plan system prompt>" },
    { "role": "user",   "content": "Local context: <非密钥摘要>\n\nTask: <自然语言任务>" }
  ],
  "stream": false
}
```

`system` 提示要求模型**只输出**一个 JSON 对象（不带 markdown 代码围栏、不带散文）。

## 响应：Plan

模型必须返回如下结构（编排层会从文本中抽取第一个 `{` 到最后一个 `}` 之间的 JSON）：

```json
{
  "actions": [
    {
      "kind": "kubectl",
      "command": "kubectl get pods -n default --field-selector=status.phase!=Running",
      "explanation": "find pods that are not Running"
    }
  ]
}
```

字段约定：

| 字段          | 取值                         | 说明                               |
|---------------|------------------------------|------------------------------------|
| `kind`        | `kubectl` \| `shell`         | 缺省按 `shell` 处理                |
| `command`     | 单条命令                     | 不得用 `&&` / `;` 串联多条         |
| `explanation` | 短句                         | 该步骤的理由，供人确认时阅读       |

## 约束

- **每个 action 只含一条命令**；多步拆成多个 action。
- 默认偏向只读 / 诊断类命令；除非用户明确要求，不得带 `--all` / `--force` / `drop` / `truncate` / `delete` 等破坏性参数。
- 无法安全映射时返回 `{"actions": []}`。

## 意图降级 (Intent Downgrade)

以下情况一律视为「降级」，编排层提示用户而**绝不**向云端求助：

- 响应中无法解析出 JSON；
- `actions` 为空数组；
- 后端本身无法处理该意图。

对应实现：`engine.ErrIntentDowngrade`。这是 Sentinel 隐私契约的硬约束。

## 安全后置校验

模型产出的每条 `command` 在执行前都会经过 Policy Guard（`internal/policy`）二次判定，
分级 `allow` / `confirm` / `block`。模型的「建议」不等于「许可」——围栏是最终关口。
