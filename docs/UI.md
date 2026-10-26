# agentd 前端视觉约定

页面观感 = 冷灰青 + 纸感 + 少量机械动效（endfield 风格）。以下规则优先级高于个人偏好，
新增样式只能落进对应小节，禁止用 `!important` 压规则。

## 1. 颜色：只有 7 级色阶 + 白
```css
--c1:#F3F5F5  --c2:#DEE4E5  --c3:#C4CECF  --c4:#99AAAD
--c5:#778587  --c6:#596364  --c7:#3A4142        --panel:#fff
```
语义映射（全部指向色阶，不要另造颜色）：

| 变量 | 值 | 用途 |
|---|---|---|
| `--paper` | c1 | 页面底 |
| `--line` | c3 | 分隔线、卡片描边 |
| `--ink` / `--ink2` / `--ink3` | c7 / c5 / c4 | 正文 / 次要 / 弱化 |
| `--theme` | c4 | 数字块底、编号底、发送按钮 |
| `--side*` | c7/c6/c1/c5 | 深色块（数字块文字、折叠把手悬停） |

规则：强调只用"底色/描边"，不改文字色去强调；一屏出现的强调色种类不超过 2。

## 2. 字体：Maple Mono NF CN（内置）
```
web/public/fonts/  MapleMono-Regular.woff2 / -Bold.woff2（官方拉丁）
                   MapleMono-CN-subset.woff2（GBK 子集）
@font-face 用 unicode-range 分工；endfield 的 --ef-font-* 也被指向它
body: 14px/1.7 "Maple Mono NF CN", ui-monospace, …
```
新增字号只在 4 档内取：`.62rem`（标签/宽体小字）、`.72rem`（次要）、`.86rem`（正文）、`1.45rem`（数字块）。

## 3. 卡片：一套结构，多处复用
```
.card       width:var(--card-w,400px); max-height:100%; 白底 1px 描边；无 !important
.card-hd    标题行（宽体小字 + 下划线），右侧放动作按钮
.card-bd    唯一滚动容器（内容超高时只滚这里）
.item      一行配置项：.item-title（可点，600）/ .item-desc（3 行限高，不可选中）
.item.static 只读项（不加粗、不随 hover 变色）
.field      表单字段：.field-label + 方角输入
.choices/.choice  多选组
.btn        切角按钮（右上切角＝可点击）
```
层级差异只体现在"位置与内容"，不体现在样式。

## 4. 布局
```
左：抽屉（默认 250，右缘 6px 透明热区可拖 200–520）
    顶部三格数字（会话/归档/本轮）→ 会话列表 → 空白区（＋新会话）→ 底条 WORKSPACE
中：消息流（卡片 + 工具记录），顶部 2px 全局进度条 = 阅读位置 / 可见轮次
右：竖排标题 + 轮次数字 + 状态 + ＋新会话 + 设置三点（≥1400px 才显示）
```
隐藏：会话区滚动条；页面无顶栏（原顶栏已移除）。

## 5. 动效
- 时长：0.12–0.3s；缓动 `cubic-bezier(.2,.8,.2,1)`；只用 transform/opacity/background。
- 已有：数字翻动（number-flow）、数字块 hover 高亮滑入、折叠把手图标旋转、
  ＋ 悬停旋转 22.5°、卡片头部按钮按需出现。
- 不做：弹跳、缩放强调、超过 0.4s 的过渡。

## 6. 文案
标签只写必要约定（如 `/v1`），常识性说明一律删（如"勾选启用""本地或云端"）。
按钮动词优先：保存 / 取消 / 关闭 / 删除 / 恢复。

## 7. 已踩过的坑
```
· !important 会压过内联样式 → 曾导致 JS 设的 height 失效
· grid-template-areas 与 grid-template-columns 列数不一致 → 整条声明被浏览器丢弃（查不到该属性）
· 给祖先加 z-index 会形成层叠上下文 → 面板被遮罩盖住，点击失效
· .ef-read / .ef-prose 自带字体变量 → 与全局字体不一致，需要同步 --ef-font-*
```
