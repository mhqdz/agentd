# 前端重构计划（架构 A：容器/展示分离 + 薄 store）

> 用途：跨会话交接。任何 agent（或压缩后的我）读完这份就能接着做，不必回看对话历史。
> 最近状态：Step 1 已完成（提交 `3e827b8`）；Step 2/3 未开始。
> 回滚点：tag `backup-web-20261030`，快照 `~/backups/agentd-web-20261030-0445.tgz`。

## 0. 目标与验收

```
目标：把 web/src/App.jsx（≈1700 行巨型组件）拆成 数据层 / 展示层 / 装配层
验收（可量化）：
  · 流式中每秒渲染 ≤10 次（当前已靠 80ms 节流 + LiveBubble 达标）
  · 历史消息在流式期间不参与 diff（Step 2 后成立）
  · 500 条会话：滚动、展开工具组、切换会话无明显掉帧
  · scripts/ui-test/{run,run2,run3}.py 全绿
```

## 1. 已完成（Step 1）

```
web/src/conversation/streamStore.js   useSyncExternalStore 的流式缓冲（get/set/patch/clear/subscribe/useLiveMsg）
App.jsx 里的接线：
  push() → stream.set({...live})，工具事件 & finish → stream.clear()
  <LiveBubble busy={busy}/>：订阅 store，只它随 delta 重渲染；无 token 时显示「等待模型响应…」
docs/UI.md §13 记录了架构与三步迁移
验证：长生成实测 store/DOM 同步增长；run/run2/run3 全绿
```

## 2. Step 2：抽 `MessageCard` + memo（下一步先做这个）

### 2.1 要搬的 JSX（当前在 `App.jsx` 的 `msgs.map(...)` 里）

```
分支顺序（不要改）：
  1) isGhostMsg(m)                    → 目前返回 null（空正文 assistant 不渲染）
  2) m.role === 'tool'                → 工具组（runStart/runStats/thinkBefore、默认收拢）
  3) isSysNote(m)                     → 系统提示（[No.xxx] + 文本 + 细线）
  4) 其它                              → 用户/Agent 气泡（头像、编号、思考折叠、正文、图片、复制）
```

### 2.2 目标文件与 props

```
web/src/components/MessageCard.jsx
  props:
    m            消息对象（已完成消息是不可变的；同一引用 ⇒ 可跳过渲染）
    seqNo        显示编号（m.seq || liveNo.get(m)）
    isFirst/last 工具组首尾（也可传 runStart 计算结果）
    think        thinkBefore(i) 的结果
    open         工具组是否展开
    onToggle     切换工具组
    visibleIdx   用于 data-vidx（阅读位置导航）
  纯展示：不读全局 state、不发请求。

web/src/components/ToolGroup.jsx  （从 MessageCard 里再拆）
web/src/components/SysNote.jsx
```

### 2.3 memo 比较函数（关键）

```js
export default React.memo(MessageCard, (a, b) =>
  a.m === b.m &&                 // 同一消息对象 ⇒ 内容没变
  a.open === b.open &&           // 工具组展开态
  a.visibleIdx === b.visibleIdx  // 阅读位置编号
)
```

### 2.4 纯函数（✅ 已完成：conversation/messageGroups.js）

```
已抽到 web/src/conversation/messageGroups.js（纯函数，msgs 作为参数传入）：
  isGhostMsg(m) / neighborRole(msgs,from,step) / runStart(msgs,i)
  runStats(msgs,start) / thinkBefore(msgs,i)
App.jsx 里只留薄包装：neighbor / runStartAt / runStatsAt / thinkBeforeAt
（抽出后 run.py、run3.py 回归均无失败）
```
⚠️ 这些函数依赖 `msgs` 数组，抽取组件时要把「计算好的结果」作为 props 传入，
不要让组件自己去访问全局 msgs（否则 memo 永远失效）。

### 2.5 迁移顺序（每步都构建 + 跑用例）

```
a. 新建 MessageCard.jsx，先只处理分支 4（用户/Agent）
b. App.jsx 的 map 改成 <MessageCard/>，删掉对应 JSX；构建 + run.py
c. 搬分支 3（系统提示）→ SysNote.jsx；构建 + run.py
d. 搬分支 2（工具组）→ ToolGroup.jsx（注意 runStart/runStats 由父层算好传入）；构建 + run3.py
e. 上 React.memo + 比较函数；用 500 条会话检查滚动与展开
```

## 3. Step 3：设置面板与 App 装配

```
设置区（当前也在 App.jsx 里，靠 editP/toolCfg/toolForm/taskDetail 等多份状态编排）：
  components/Settings/ListCard.jsx     常规配置 + 模型列表 + 工具列表 + 任务列表
  components/Settings/ModelForm.jsx
  components/Settings/ToolForm.jsx
  components/Settings/EditorCard.jsx   三级编辑（system / 工具说明）
  container：settings/useSettings.js（状态机 + 保存/取消/脏检查）
App.jsx 最终只剩：布局（drawer/stage/rail）、URL 深链、快捷键、装配
```

## 4. 已知坑（改之前必读）

```
1. 空正文 assistant（工具轮）必须跳过：否则每轮工具之间多一个空 Agent 卡，
   而且会把相邻工具卡隔开、无法合并成一个组
2. 工具组起点要跳过 ghost（见 runStart），组内 Tool 头只在 first 渲染；
   收拢时也要加 run-end（否则没有下边距）
3. liveNo 只给"还没落库的实时消息"用；Step 1 之后实时消息只存在于 streamStore，
   历史消息都有 seq，所以 MessageCard 可以只收 seqNo
4. data-vidx 依赖 visible 的下标，阅读位置/右侧缩略导航都靠它；memo 时要比较
5. 滚动跟随：stickRef（用户上滚立即解除，回到底部 ≤8px 恢复），
   运行中每帧 rAF 只在 stickRef=true 时校正；不要改回"宽容区"阈值
6. 工具/系统提示的样式细节都在 theme.css 的 .toolnote/.tn-*/.sysnote 里
```

## 5. 测试与命令

```bash
cd web && npm run build                       # 前端
cd ~/Workspace/agentd && go test ./...        # 后端
/tmp/geckodriver --port 4444 &                # UI 自动化依赖
cd /tmp/uitest && python3 run.py && python3 run2.py && python3 run3.py
# 截图落在 /tmp/uitest/*.png
```

## 6. 下一步第一件事（checklist）

```
[ ] 确认 geckodriver 可用、三组用例当前全绿
[ ] 新建 components/MessageCard.jsx，迁移分支 4（用户/Agent 气泡）
[ ] App.jsx map 改用它，删旧 JSX，构建 + run.py
[ ] 逐个迁移 3 与 2，每步跑对应用例
[ ] 上 memo 比较函数，用 500 条会话验证滚动/展开
[ ] Step 3 按 §3 拆设置面板
```
