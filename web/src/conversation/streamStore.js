// 流式缓冲：放在 React 之外，只有订阅者（实时气泡/状态栏）重渲染，
// 历史消息列表不再随每个 delta 重算。
// 约定：store 里只放"尚未落库的当前一轮"；落库/切换会话时 clear()。
import { useSyncExternalStore } from 'react'

let live = null                 // { role:'assistant', content, reasoning, rsecs, at }
const subs = new Set()

export const stream = {
  get: () => live,
  set(v) { live = v; subs.forEach(f => f()) },
  patch(patch) { live = { ...(live || {}), ...patch }; subs.forEach(f => f()) },
  clear() { live = null; subs.forEach(f => f()) },
  subscribe(f) { subs.add(f); return () => subs.delete(f) },
}

// 组件里用：const live = useLiveMsg()
export function useLiveMsg() {
  return useSyncExternalStore(stream.subscribe, stream.get, stream.get)
}

// 调试用（浏览器控制台可见）：__stream.get()
if (typeof window !== 'undefined') window.__stream = stream
