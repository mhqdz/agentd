# agentd 计划与待办

## 一、工具/插件体系（设计已定，暂不实现）

现状：工具全部是 Go 内置（`internal/tools/tools.go` 的 `Registry()`，一条 Spec = name/description/
JSON Schema/Required/Run）。加工具必须改代码 + 重新编译。

决策（2026-10-25）：**不引入插件体系**，保持内置平铺。理由：Go 的 `plugin` 包只有 Linux、
要求构建参数完全一致、不能卸载；要达到 DSH（npm 包 + YAML 预设）那种效果只能走"独立进程 + 协议"，
当前 4 个工具的体量不值得付出这层复杂度。

若将来要做，按下面这套（已调研，直接可落地）：

### 形态
- 清单：`~/.data/agentd/tools.d/*.json`，字段对齐 MCP 的 tools/list：
  `{ "name", "description", "params": <JSON Schema>, "command": [...], "env": {...},
     "timeoutMs": 120000, "persistent": false }`
- 调用（一次性）：启动进程 → stdin 写 `{"tool","args","env":{"conv_id","cwd"}}` → 读 stdout 文本；
  退出码非 0 时把 stderr + `[exit N]` 作为结果（与 bash 一致）。
- 调用（常驻）：`persistent:true` 时走 JSON-RPC over stdio（initialize / tools/list / tools/call），
  宿主负责生命周期与崩溃重启。
- 作用域：仍由 `profiles.json` 的 `tools` 数组决定暴露给谁。
- 与 DSH 的对应：清单 ≈ 插件包声明，`params` ≈ schemastery/zod schema，进程 ≈ 插件实例。

### 不采用
- Go `plugin`（.so）：平台与构建耦合，不可热重载。
- 在配置里表达分支逻辑：配置只声明"怎么调"，逻辑留在进程内（脚本/服务）。

## 二、其他待办
- 工具说明再压缩（`recall` 649 字符 → ~200）以省上下文。
- 归档页「恢复」图标已做；删除会话 UI 尚未提供。
- 设置里显示"上次自动归档时间/数量"。
- 消息重试、会话重命名。
