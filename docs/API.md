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
- `max` 是该 profile 的上限：`profiles.json` 的 `max_prompt_chars`，留空/0 时用内置默认 240000
- `compact_pct` / `compact_at`：达到上限的百分之几（默认 80）时触发自动压缩/摘要，以及对应的字符数
- 全局不再维护上限与阈值：都写在每个 profile 里（`max_prompt_chars`、`compact_pct`），超过即中止该轮并提示新开会话
- `raw` 是未压缩的原始体量（消息正文 + 思考），用于判断"对话本身有多大"
- `summary` 表示该会话已有压缩摘要

前端在状态栏显示：`上下文 5.0k / 240k（2.1%） 原 214k`，达到压缩阈值时变深加粗；鼠标悬停会提示"达到 80%（约 192k）会自动压缩/摘要"。
