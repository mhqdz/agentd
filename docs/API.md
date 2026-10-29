# agentd 接口文档

约定：全部 JSON（除 SSE 与静态文件）；错误统一 `{"error":"说明"}` + 相应状态码；无鉴权（仅监听 127.0.0.1）。

## 健康与运行态

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | `{"ok":true}` |
| GET | `/api/running` | 正在生成中的会话 id 数组，如 `["8","9"]` |

## 会话

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/convs` | 未归档列表：`[{ID,Title,UpdatedAt,N}]`（N=消息条数） |
| GET | `/api/convs?archived=1` | 归档列表，同结构 |
| GET | `/api/conv/{id}` | 会话全文：`{id,title,created_at,updated_at,messages:[{seq,role,content,reasoning,reasoning_ms,tool_calls,tool_result,created_at}]}` |
| POST | `/api/conv/{id}/archive`（DELETE 同效） | 归档 |
| POST | `/api/conv/{id}/restore` | 恢复 |
| POST | `/api/conv/{id}/interject` `{"text":"…"}` | 向进行中的轮次插话（下一轮工具调用前注入） |
| POST | `/api/conv/{id}/stop` | 中断当前生成（后台任务取消，已生成内容落库） |
| GET | `/api/conv/{id}/stream?from=N` | 断线重连：重放缓冲区第 N 字节起并续流；不在运行且无缓冲时 404 |

## 对话（SSE）

`POST /api/chat` `{"conv_id":"","text":"…","profile":"180-low"}`

- 立即返回 `text/event-stream`；后台任务不绑定 `r.Context()`，浏览器断开不中断生成。
- 事件：

| event | data | 说明 |
|---|---|---|
| `meta` | `{conv_id,title}` | 会话已创建/确定，前端据此写 URL |
| `reasoning` | `{text}` | 思考增量 |
| `delta` | `{text}` | 正文增量 |
| `tool` | `{name,args,text,image}` | 一次工具调用结束（text=输出摘要，image=是否含图） |
| `interject` | `{text}` | 已收到插话 |
| `error` | `{error}` | 出错（已在会话里落半截内容 + 中断说明） |
| `done` | `{conv_id,secs}` | 完成 |

## 设置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/settings` | `{tool_rounds_enabled,tool_rounds,auto_archive_enabled,auto_archive_days}` |
| PUT | `/api/settings` | 同上结构；**自动归档相关字段变化时**会立即触发一次巡检 |

## 模型与 profile

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/models?base_url=&api_key=` | 代理上游 `/v1/models`：`{"models":["…"]}`；非 200 时返回 `{"error":"接口返回 N（检查地址或 API Key）"}` |
| GET | `/api/profiles` | profile 数组 |
| PUT | `/api/profiles/{name}` | 新建或更新（`api_key` 为空则回落全局配置） |
| DELETE | `/api/profiles/{name}` | 删除（内置也可删） |

## 工具

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/tools` | `[{name,description,params,required}]` |
| PUT | `/api/tools/{name}` | `{"description":"…"}` 覆盖说明（写 `~/.data/agentd/tools.json`） |

## 静态

`GET /` 及其它未匹配路径 → `web/dist`（`Cache-Control: no-store` 对 html）；`/fonts/*`、`/assets/*` 直出。

## 示例

```bash
curl -s http://127.0.0.1:18400/api/settings
curl -s -N -X POST http://127.0.0.1:18400/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"conv_id":"","text":"列出当前目录","profile":"180-low"}'
curl -s http://127.0.0.1:18400/api/conv/9 | jq '.messages[-1]'
```

## 工具

`GET /api/tools` → 每个工具含 `name / description / params / source / path`：

```json
[{"name":"bash","source":"builtin","path":"","timeout_ms":0,"persistent":false},
 {"name":"kb_search","source":"plugin","path":"~/.data/agentd/tools.d/kb_search.json","timeout_ms":20000,"persistent":false}]
```

- `source=builtin`：Go 内置（`internal/tools/tools.go` 里用 Spec 指定），编译期确定
- `source=plugin`：外置插件（`~/.data/agentd/tools.d/*.json` 清单 + 可执行脚本），改完即生效、无需重编译

清单格式：

```json
{"name":"kb_get","description":"…","params":{…},"required":["id"],
 "command":["~/Workspace/agentd/scripts/tools/kb_get"],"timeoutMs":20000}
```

常驻形态：清单加 `"persistent": true` → 进程只启动一次，走 JSON-RPC 2.0 over stdio
（`initialize` / `tools/list` / `tools/call`），**一个进程可以暴露多个工具**（与 MCP 同构）。
`tools/list` 返回的 `{name,description,params,required}` 会覆盖清单里的同名字段；
结果支持 `{"content":[{"type":"text","text":…},{"type":"image","url":"data:…"}]}`。
示例：`scripts/tools/demo_persist.py` + `scripts/tools.d/demo_persist.json.example`。

一次性形态：直接 exec（不经 shell）；参数以 `AGENTD_ARG_<NAME>` 环境变量传入（数组用换行连接），
stdin 同时给一份 `{"tool":"…","args":{…}}`；stdout 为纯文本，或 `{"text":…,"images":[…]}`。
可用性仍由 `profiles.json.tools` 决定。

### 插件的增删

| 方法 | 路径 | 说明 |
|---|---|---|
| POST/PUT | `/api/plugins` | 保存清单（name/description/command/timeoutMs/persistent/params/required）；同名内置工具会被拒绝 |
| DELETE | `/api/plugins/<name>` | 删除清单（只删 `~/.data/agentd/tools.d/<name>.json`，脚本文件保留）；内置工具拒绝 |

`PUT /api/tools/<name> {"description":"…"}`：内置工具写 `tools.json` 覆盖；外置插件直接改清单里的 description。

## 截图文件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/shots/stats` | `{count, size_mb}`：截图目录的文件数与占用 |
| POST | `/api/shots/cleanup` `{"days":7}` | 删除超过 N 天的截图，返回 `{removed, freed_kb, days}` |
| GET | `/shots/<name>.png` | 直接取截图文件（`~/.data/agentd/shots/`） |

消息里只存 `/shots/<name>.png` 路径；发给模型时由 `toWire` 读盘转 data URL。
自动清理：`settings.shots_auto_clean` + `shots_keep_days`（默认开 + 7 天），
启动与每小时巡检时执行一次。

## 上下文占用

`GET /api/context?conv=<id>&profile=<name>`

```json
{"chars":4061, "max":240000, "pct":1.69, "raw":213204, "messages":136, "summary":true}
```

- `chars` 是**实际会发给模型的字符数**（已按 keep_messages 压缩、已含 system 与摘要）
- 口径统一为 **token**：`profiles.json` 的 `max_prompt_tokens`，留空/0 用内置默认 105000
- `limit_tokens` 是该 profile 的输入上限（`max_prompt_tokens`，默认 105000）
- `window` 是模型窗口：优先 `profiles.json.context_window`；否则**自动读上游 `GET /v1/models` 的 `meta.n_ctx`**（10 分钟缓存）；都拿不到才用 131072
- `chars` / `max` 是内部换算值（1 token ≈ 1.8 字符），仅用于实际裁剪
- 例：`{"est_tokens":29096,"limit_tokens":105000,"window":131072,"compact_pct":80,"compact_at":84000,"chars":52373,"max":189000}`
- `compact_pct` / `compact_at`：达到上限的百分之几（默认 80）时触发自动压缩/摘要，以及对应的字符数
- 全局不再维护上限与阈值：都写在每个 profile 里（`max_prompt_chars`、`compact_pct`），超过即中止该轮并提示新开会话
- `raw` 是未压缩的原始体量（消息正文 + 思考），用于判断"对话本身有多大"
- `summary` 表示该会话已有压缩摘要

前端在状态栏显示：`上下文 5.0k / 240k（2.1%） 原 214k`，达到压缩阈值时变深加粗；鼠标悬停会提示"达到 80%（约 192k）会自动压缩/摘要"。
