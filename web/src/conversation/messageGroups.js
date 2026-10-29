// 消息分组/统计的纯函数（无 React、无副作用）。Step 2 的前置：
// 抽组件时把这些"算好的结果"作为 props 传入，组件内部不再访问全局 msgs，
// 否则 React.memo 永远失效。

export const isGhostMsg = (m) =>
  !!m && m.role === 'assistant' && !(m.content || '').trim() && !m.__live

// 从 from 出发按 step 找相邻（跳过不渲染的空正文 assistant）的角色
export function neighborRole(msgs, from, step) {
  for (let k = from; k >= 0 && k < msgs.length; k += step) {
    const m = msgs[k]
    if (isGhostMsg(m)) continue
    return m.role
  }
  return null
}

// 工具组起点：跳过组前的空正文 assistant，落在第一个 tool 上
export function runStart(msgs, i) {
  let k = i
  while (k - 1 >= 0 && (msgs[k - 1].role === 'tool' || isGhostMsg(msgs[k - 1]))) k--
  while (k < msgs.length && isGhostMsg(msgs[k])) k++
  return k
}

// 组内统计：条数 / 总字数 / 思考段数 / 编号范围
export function runStats(msgs, start) {
  let n = 0, chars = 0, ms = 0, thinks = 0, from = 0, to = 0
  for (let k = start; k < msgs.length; k++) {
    if (isGhostMsg(msgs[k])) { if ((msgs[k].reasoning || '').trim()) thinks++; continue }
    if (msgs[k].role !== 'tool') break
    n++; chars += (msgs[k].content || '').length; ms += msgs[k].ms || 0
    const no = msgs[k].seq || 0
    if (no) { if (!from) from = no; to = no }
  }
  return { n, chars, ms, thinks, from, to }
}

// 这条工具前面紧挨着的思考（可能多段）
export function thinkBefore(msgs, i) {
  let k = i - 1
  while (k >= 0 && isGhostMsg(msgs[k])) k--
  const out = []
  for (let j = k + 1; j < i; j++) if ((msgs[j].reasoning || '').trim()) out.push(msgs[j].reasoning)
  return out.join('\n')
}
