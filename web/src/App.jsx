import React, { useEffect, useMemo, useRef, useState } from 'react'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import NumberFlow from '@number-flow/react'

marked.setOptions({ gfm: true, breaks: true })

// 日历式翻数：数字变化时逐位滚动（+1 向上，-1 向下）
function Roll({ value, digits = 2 }) {
  const txt = String(value ?? 0).padStart(digits, '0')
  const prevRef = useRef(txt)
  const prev = prevRef.current.length === txt.length ? prevRef.current : txt
  const changed = Number(txt) !== Number(prev)
  const up = Number(txt) > Number(prev)
  useEffect(() => { prevRef.current = txt }, [txt])
  return (
    <b className="roll">
      {txt.split('').map((d, i) => {
        if (!changed || prev[i] === d) return <span className="rw" key={'s' + i + d}>{d}</span>
        return (
          <span className="rw" key={'r' + i + txt}>
            <span className={'rd ' + (up ? 'up' : 'down')}><i>{d}</i></span>
          </span>
        )
      })}
    </b>
  )
}

// 数字格：number-flow 管逐位滚动；外层 figIn 管入场
// 关键在于**不重挂载**（重挂载会让 number-flow 丢失旧值 → 不滚）
// 用 class 重播动画（remove → 强制回流 → add）
function FigCell({ value }) {
  const box = useRef(null)
  const prev = useRef(value)
  useEffect(() => {
    if (prev.current === value) return
    const el = box.current
    if (el) {
      el.classList.remove('figIn')
      void el.offsetWidth
      el.classList.add('figIn')
    }
    prev.current = value
  }, [value])
  return (
    <span className="nfwrap" ref={box}>
      <NumberFlow className="nf" value={value} format={{ minimumIntegerDigits: 2 }} />
    </span>
  )
}

// Markdown 渲染（模型输出不可信 → 一律过 DOMPurify）
function Md({ text }) {
  const html = useMemo(() => DOMPurify.sanitize(marked.parse(String(text || ''))), [text])
  return <div className="prose ef-prose md" dangerouslySetInnerHTML={{ __html: html }} />
}

const api = (p, opt) => fetch(p, opt).then(r => r.json())
const pad = n => String(n).padStart(2, '0')
const now = () => new Date().toTimeString().slice(0, 5)

// 最后活跃时间：今天显示 HH:MM，否则 MM.DD
const fmtActive = (iso) => {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d)) return String(iso).slice(5, 10).replace('-', '.')
  const t = new Date()
  const sameDay = d.toDateString() === t.toDateString()
  const pad = n => String(n).padStart(2, '0')
  return sameDay ? pad(d.getHours()) + ':' + pad(d.getMinutes())
                 : pad(d.getMonth() + 1) + '.' + pad(d.getDate())
}



function ProfileSeg({ profiles, value, onChange, small }) {
  return (
    <div className="ef-seg" role="radiogroup" aria-label="profile" style={small ? { height: 'auto' } : undefined}>
      {profiles.map(p => (
        <label key={p.name} title={p.model}>
          <input type="radio" name={'seg-' + (small ? 's' : 'l')} checked={p.name === value}
            onChange={() => onChange(p.name)} />
          <span>{p.name}</span>
        </label>
      ))}
    </div>
  )
}

// 历史消息归一：把 assistant.tool_calls 与其后的 tool 结果合成 tools 数组，
// 这样刷新后"思考过程"和"工具调用记录"不会消失（之前只存在于流式时的前端状态 ✗）
function normalize(arr) {
  // 工具结果保留为独立消息（有自己的 seq，界面上是单独一个气泡）
  const call = {}
  for (const m of arr) {
    for (const tc of (m.tool_calls || [])) {
      call[tc.id] = { name: tc.function?.name || '工具', args: tc.function?.arguments || '' }
    }
  }
  return arr.map(m => {
    if (m.role === 'tool') {
      const c = call[m.tool_call_id] || {}
      return { ...m, toolName: c.name, toolArgs: c.args }
    }
    const { tools, ...rest } = m    // assistant 不再内嵌工具列表
    return rest
  })
}


function EditorCol({ value, onChange, onDone, onCancel, placeholder }) {
  const ta = useRef(null)
  useEffect(() => {
    const el = ta.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = (el.scrollHeight + 2) + 'px'   // 新样式表无 !important，内联生效 ✓
  }, [value])
  return (
    <div className="card col-editor">
      <textarea ref={ta} className="set-area" value={value || ''} placeholder={placeholder}
        onChange={e => onChange(e.target.value)} />
      <div className="set-col-foot">
        <span className="set-meta" style={{ marginRight: 'auto' }}>{(value || '').length} 字</span>

      </div>
    </div>
  )
}

// 把回复按 markdown 标题切成"01 / 标题"编号段落；无标题则整段输出
function Sections({ text }) {
  const lines = String(text || '').split('\n')
  const out = []
  let buf = [], n = 0
  const flush = () => {
    if (buf.length) {
      const body = buf.join('\n').trim()
      if (body) out.push(<Md text={body} key={'b' + out.length} />)
      buf = []
    }
  }
  for (const raw of lines) {
    const m = /^#{1,4}\s+(.*)$/.exec(raw.trim())
    if (m) {
      flush(); n++
      out.push(
        <div className="sec" key={'h' + n}>
          <span className="secno">{pad(n)}</span>
          <span className="secti">{m[1]}</span>
        </div>)
    } else buf.push(raw)
  }
  flush()
  if (out.length === 0) return <Md text={text} />
  return <>{out}</>
}

export default function App() {
  const [convs, setConvs] = useState([])
  const [archived, setArchived] = useState([])
  const [profiles, setProfiles] = useState([])
  const [profile, setProfile] = useState(localStorage.getItem('profile') || '180-think')
  const [tab, setTab] = useState('live')
  const [sideOpen, setSideOpen] = useState(localStorage.getItem('sideOpen') !== '0')
  const [sideW, setSideW] = useState(() => Number(localStorage.getItem('sideW')) || 250)
  const [colW, setColW] = useState(() => Number(localStorage.getItem('colW')) || 960)
  const colDrag = useRef(null)
  const stageRef = useRef(null)
  const setOpenRef = useRef(false)
  const threadRef = useRef(null)
  const [inputH, setInputH] = useState(() => Number(localStorage.getItem('inputH')) || 92)
  const hDrag = useRef(null)
  const abortRef = useRef(null)
  const [aborted, setAborted] = useState(false)
  const [showSet, setShowSet] = useState(new URLSearchParams(location.search).has('settings'))
  const [editP, setEditP] = useState(null)
  const [toolList, setToolList] = useState([])
  const [settings, setSettings] = useState({ tool_rounds_enabled: true, tool_rounds: 8 })
  const [toolEdit, setToolEdit] = useState(null)
  const [toolCfg, setToolCfg] = useState(null)   // 工具配置卡（开关/子工具）
  const [expandedTool, setExpandedTool] = useState(null)
  const [models, setModels] = useState({ loading: false, list: [], err: '' })
  const [readingIdx, setReadingIdx] = useState(0)
  const [peek, setPeek] = useState({ open: false, idx: -1, top: 0 })
  const suppressPeekClose = useRef(false)
  const suppressTimer = useRef(null)
  const [editField, setEditField] = useState(null)
  const [editOriginal, setEditOriginal] = useState('')
  const [editSnap, setEditSnap] = useState('')
  const [setSnap, setSetSnap] = useState(() => JSON.stringify({ tool_rounds_enabled: true, tool_rounds: 8 }))

  // 快照去掉内部标记 _new，避免"一打开就算脏"✗
  const snapOf = (p) => { if (!p) return ''; const { _new, ...rest } = p; return JSON.stringify(rest) }
  const formDirty = !!editP && snapOf(editP) !== editSnap
  const editorDirty = (editField || toolEdit)
    ? ((editField ? (editP?.[editField] || '') : (toolEdit?.description || '')) !== (editOriginal || ''))
    : false
  const settingsDirty = JSON.stringify(settings) !== setSnap
  const anyDirty = formDirty || editorDirty || settingsDirty
  const drag = useRef(null)
  const [convId, setConvId] = useState(null)
  const [title, setTitle] = useState('新会话')
  const [msgs, setMsgs] = useState([])
  const visible = msgs.filter(m => m.role !== 'tool')   // 工具结果不计入轮次 ✓
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [status, setStatus] = useState('')
  const [secs, setSecs] = useState(null)
  const endRef = useRef(null)

  useEffect(() => { localStorage.setItem('sideW', String(sideW)) }, [sideW])

  useEffect(() => { localStorage.setItem('colW', String(colW)) }, [colW])

  useEffect(() => { localStorage.setItem('inputH', String(inputH)) }, [inputH])

  // 右上角小三角：拖动调整输入框高度（替代原生右下角 resize ✓）
  const startResize = (e) => {
    e.preventDefault(); e.stopPropagation()
    hDrag.current = { y: e.clientY, h: inputH }
    const move = (ev) => {
      const h = Math.min(420, Math.max(56, hDrag.current.h - (ev.clientY - hDrag.current.y)))
      setInputH(h)
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      document.body.style.cursor = ''; document.body.style.userSelect = ''
    }
    document.body.style.cursor = 'ns-resize'; document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', move); window.addEventListener('mouseup', up)
  }

  // 插话：把内容压进后端队列，模型在下一个工具轮次就能看到（不中断当前生成）
  const interject = async () => {
    const t = text.trim()
    if (!t) return
    if (!convId) { setStatus('这一轮还没拿到会话 id，稍等一下再插话'); return }
    const r = await fetch('/api/conv/' + convId + '/interject', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: t }),
    })
    if (!r.ok) { setStatus('✗ 插话失败'); return }
    setText('')
    setMsgs(m => [...m, { role: 'user', content: t, at: now(), interject: true }])
    setStatus('已插话：模型会在本轮处理它')
  }

  // 停止生成：中断 fetch → 后端 r.Context() 被取消 → 上游请求结束
  const stop = () => {
    if (convId) fetch('/api/conv/' + convId + '/stop', { method: 'POST' }).catch(() => {})
    abortRef.current?.abort()
    abortRef.current = null
    setAborted(true)
    setStatus('已停止（已保留已生成的内容）')
  }

  // 滚轮：在输入区/状态栏上滚动也归给会话区（否则滚轮在这里"失灵"）
  // 只在输入区/状态栏上帮会话滚（不挂在 .stage 上，避免误伤设置面板等子层 ✓）
  const onComposerWheel = (e) => {
    const th = stageRef.current?.querySelector('.thread')
    if (!th) return
    th.scrollTop += e.deltaY
  }

  // 取装饰线的实际横坐标（.col 左缘再往左 1.4rem）
  const lineX = () => {
    const col = stageRef.current?.querySelector('.thread .col')
    return col ? col.getBoundingClientRect().left - 22 : -999
  }

  // 鼠标靠近：按距离设置不透明度（0 → 1，无生硬切换）
  const onStageMove = (e) => {
    const stage = stageRef.current
    const col = stage?.querySelector('.thread .col')
    if (!stage || !col) return
    const cr = col.getBoundingClientRect(), sr = stage.getBoundingClientRect()
    const dx = Math.abs(e.clientX - (cr.left - 22))
    const a = Math.max(0, Math.min(1, 1 - (dx - 6) / 54))     // 横向：近线才显示
    stage.style.setProperty('--lineA', String(a))
    stage.style.setProperty('--lineX', (cr.left - 22 - sr.left) + 'px')
    stage.style.setProperty('--lineY', (e.clientY - sr.top) + 'px')   // 纵向：可见窗口跟随鼠标
    stage.style.cursor = dx <= 10 ? 'col-resize' : ''
  }

  const onStageLeave = () => {
    if (stageRef.current) {
      stageRef.current.style.setProperty('--lineA', '0')
      stageRef.current.style.cursor = ''
    }
  }

  // 只在贴近线时才允许拖动
  const onStageDown = (e) => {
    if (Math.abs(e.clientX - lineX()) <= 10) startColDrag(e)
  }

  // 拖动阅读栏左边界调整正文宽度（居中 → 左右留白同步变化）
  const startColDrag = (e) => {
    e.preventDefault()
    colDrag.current = { x: e.clientX, w: colW }
    const move = (ev) => {
      const dx = colDrag.current.x - ev.clientX
      const w = Math.min(1400, Math.max(560, colDrag.current.w + dx * 2))
      setColW(w)
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  // 拖动抽屉右缘调整宽度（200 ~ 520）
  const startDrag = (e) => {
    e.preventDefault()
    drag.current = { x: e.clientX, w: sideW }
    const move = (ev) => {
      const w = Math.min(520, Math.max(200, drag.current.w + (ev.clientX - drag.current.x)))
      setSideW(w)
    }
    const up = () => {
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }

  // 逐级退出：编辑器 → 详情 → 设置；有改动先问是否保存
  // 文本域随内容长高（写 CSS 变量，避免被 !important 压掉）
  const autoGrow = (el) => {
    if (!el) return
    el.style.height = 'auto'
    el.style.setProperty('--h', (el.scrollHeight + 2) + 'px')
  }

  // 阅读位置：滚到哪条消息，就显示第几条
  const onThreadScroll = () => {
    const box = threadRef.current
    if (!box) return
    const top = box.getBoundingClientRect().top
    const bottom = top + box.clientHeight
    let idx = 0
    box.querySelectorAll('[data-vidx]').forEach(el => {
      if (el.getBoundingClientRect().top < bottom) idx = Number(el.dataset.vidx) || idx
    })
    setReadingIdx(idx)
    if (suppressPeekClose.current) {
      clearTimeout(suppressTimer.current)
      suppressTimer.current = setTimeout(() => { suppressPeekClose.current = false }, 260)
    } else {
      setPeek(p => (p.open ? { open: false, idx: -1, top: 0 } : p))
    }
  }


  // 缩略图导航：悬停 36/36 → 右侧栏左侧弹出 5 条，中心对应当前阅读位置
  const idxOfSeq = (seq) => visible.findIndex(m => (m.seq || 0) === seq)
  const curIdx = () => {
    return Math.max(0, Math.min(visible.length - 1, (readingIdx || 1) - 1))
  }
  const peekRows = (() => {
    if (!peek.open || !visible.length) return []
    const center = peek.idx >= 0 ? peek.idx : curIdx()
    const out = []
    for (let d = -2; d <= 2; d++) {
      const i = center + d
      if (i >= 0 && i < visible.length) out.push({ i, m: visible[i] })
    }
    return out
  })()
  const scrollToMsg = (i) => {
    const el = document.querySelector('[data-vidx="' + (i + 1) + '"]')
    if (!el) return
    suppressPeekClose.current = true
    clearTimeout(suppressTimer.current)
    suppressTimer.current = setTimeout(() => { suppressPeekClose.current = false }, 600)
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }

  const roundMax = visible.length
  const roundNow = readingIdx || visible.length

  const closeOne = async () => {
    const open = editField || toolEdit
    if (open) {
      const cur = editField ? (editP?.[editField] || '') : (toolEdit?.description || '')
      if (cur !== (editOriginal || '')) {
        if (!confirm('文本已修改，是否保存？\n确定 = 保存并关闭　取消 = 放弃修改')) {
          if (editField) setEditP({ ...editP, [editField]: editOriginal })
          else setToolEdit({ ...toolEdit, description: editOriginal })
        } else if (toolEdit) {
          await fetch('/api/tools/' + toolEdit.name, { method: 'PUT', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ description: toolEdit.description }) })
          refresh()
        }
      }
      if (editField) setEditField(null); else setToolEdit(null)
      return
    }
    if (editP) { setEditP(null); return }
    setShowSet(false)
  }

  const refresh = async () => {
    setConvs(await api('/api/convs'))
    setArchived(await api('/api/convs?archived=1').catch(() => []))
    setProfiles(await api('/api/profiles'))
    setToolList(await api('/api/tools').catch(() => []))
    const st = await api('/api/settings').catch(() => ({ tool_rounds_enabled: true, tool_rounds: 8 }))
    setSettings(st); setSetSnap(JSON.stringify(st))
  }
  useEffect(() => {
    refresh().then(async () => {
      const q = new URLSearchParams(location.search)
      const c = q.get('conv')                                     // ?conv=<id> 深链
      if (c) openConv(c)
      const pn = q.get('profile')                                 // ?settings=1&profile=9-exec 直接打开编辑
      if (pn) {
        const list = await api('/api/profiles')
        const pr = (list || []).find(x => x.name === pn)
        if (pr) setEditP({ ...pr, tools: (pr.tools || []).join(',') })
      }
    })
  }, [])
  useEffect(() => { setOpenRef.current = showSet }, [showSet])
  useEffect(() => {
    const el = document.querySelector('.col-editor .set-area')
    if (el) setTimeout(() => autoGrow(el), 0)
  }, [editField, toolEdit])
  useEffect(() => { localStorage.setItem('profile', profile) }, [profile])
  useEffect(() => { localStorage.setItem('sideOpen', sideOpen ? '1' : '0') }, [sideOpen])
  // 自动跟随：仅当用户已在底部附近才滚到底；往上翻阅读历史时不打断
  useEffect(() => {
    const th = threadRef.current
    if (!th) return
    const nearBottom = th.scrollHeight - th.scrollTop - th.clientHeight < 120
    if (nearBottom) th.scrollTop = th.scrollHeight
  }, [msgs, status])

  // 续流去重：DB 里已落库的那条（本轮进行中的 assistant）不该与流式副本并存
  const trimDup = (list, live) => {
    const out = [...list]
    while (out.length) {
      const last = out[out.length - 1]
      const c = last && last.role === "assistant" ? (last.content || "") : null
      if (c !== null && (c === "" || (live.content || "").startsWith(c) || /——（中断/.test(c))) out.pop()
      else break
    }
    return out
  }

  // 滚到会话底部（新消息/进入会话时用）
  const scrollToBottom = () => {
    requestAnimationFrame(() => {
      const th = threadRef.current
      if (th) th.scrollTop = th.scrollHeight
    })
  }

  const openConv = async (id) => {
    setConvId(id)
    // A：会话 id 进 URL，刷新后能回到同一会话（也便于分享/重开）
    if (id) history.replaceState(null, '', '?conv=' + id)
    else history.replaceState(null, '', location.pathname)
    if (!id) { setMsgs([]); setTitle('新会话'); return }
    const c = await api('/api/conv/' + id)
    setMsgs(normalize(c.messages || [])); setTitle(c.title || '会话')
    scrollToBottom()   // 进入会话定位到最新消息
    // 后台仍在跑？轮询直到结束（刷新页面后能"接上"）
    // 单一事实来源：直接请求续传流。没内容/已结束会立刻返回，不会挂着
    attachStream(id)
  }


  // D：SSE 断点续传 —— 刷新后从后端缓冲继续接收（不在前端存状态）
  const attachStream = async (id) => {
    let live = { role: 'assistant', content: '', reasoning: '', tools: [], at: now() }
    let base = null
    const push = () => setMsgs(m => {
      const kept = trimDup(m.filter(x => !x.__live), live)
      return [...kept, { ...live, tools: [...live.tools], __live: true }]
    })
    try {
      const resp = await fetch('/api/conv/' + id + '/stream?from=0')
      if (!resp.ok) { setSyncing(false); setStatus('已同步'); return }
      const reader = resp.body.getReader(); const dec = new TextDecoder()
      let buf = ''
      // 看门狗：8 秒没有任何帧且已经不在运行 → 认为结束，避免"正在接上…"卡死
      let lastFrame = Date.now()
      const guard = setInterval(async () => {
        if (Date.now() - lastFrame < 8000) return
        const r2 = await api('/api/running').catch(() => [])
        if (!(r2 || []).includes(String(id))) {
          clearInterval(guard)
          const cc = await api('/api/conv/' + id).catch(() => null)
          if (cc) setMsgs(normalize(cc.messages || []))
          setSyncing(false); setStatus('已同步'); refresh()
        }
      }, 2000)
      for (;;) {
        const { value, done } = await reader.read(); if (done) break
        buf += dec.decode(value, { stream: true })
        let i
        while ((i = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, i); buf = buf.slice(i + 2)
          let ev = '', data = ''
          for (const line of block.split('\n')) {
            if (line.startsWith('event: ')) ev = line.slice(7)
            else if (line.startsWith('data: ')) data = line.slice(6)
          }
          let d = {}; try { d = JSON.parse(data) } catch { }
          lastFrame = Date.now()          // 心跳也算活动 ✓
          if (ev) lastFrame = Date.now()
          if (ev === 'reasoning') { live.reasoning += d.text || ''; push() }
          else if (ev === 'delta') { live.content += d.text || ''; push() }
          else if (ev === 'tool') { live.tools.push(d); push() }
          else if (ev === 'done') { /* 结束时统一重载 */}
        }
      }
      clearInterval(guard)
      const cc = await api('/api/conv/' + id).catch(() => null)
      if (cc) setMsgs(normalize(cc.messages || []))
      setSyncing(false); setStatus('已同步'); refresh()
    } catch (e) {
      setSyncing(false); setStatus('同步失败：' + e.message)
    }
  }

  const send = async () => {
    const t = text.trim(); if (!t || busy) return
    setText('')
    sendText(t)
  }

  const sendText = async (t) => {
    if (!t || busy) return; setBusy(true); setStatus(''); setSecs(null); setAborted(false)
    const ac = new AbortController()
    abortRef.current = ac
    setMsgs(m => [...m, { role: 'user', content: t, at: now() }])
    scrollToBottom()   // 发送后回到最新位置
    let cur = convId
    try {
      const resp = await fetch('/api/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conv_id: cur || '', text: t, profile }),
        signal: ac.signal,
      })
      const reader = resp.body.getReader(); const dec = new TextDecoder()
      let buf = '', asst = { role: 'assistant', content: '', reasoning: '', tools: [], at: now() }
      const push = () => setMsgs(m => [...m.filter(x => !x.__live), { ...asst, tools: [...asst.tools], __live: true }])
      for (;;) {
        const { value, done } = await reader.read(); if (done) break
        buf += dec.decode(value, { stream: true })
        let i
        while ((i = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, i); buf = buf.slice(i + 2)
          let ev = '', data = ''
          for (const line of block.split('\n')) {
            if (line.startsWith('event: ')) ev = line.slice(7)
            else if (line.startsWith('data: ')) data = line.slice(6)
          }
          let d = {}; try { d = JSON.parse(data) } catch { }
          if (ev === 'meta') {
            cur = d.conv_id; setConvId(cur)
            if (cur) history.replaceState(null, '', '?conv=' + cur)   // A：新会话也写进 URL
            if (d.title) setTitle(d.title)
          }
          else if (ev === 'reasoning') {
            if (!asst._r0) asst._r0 = Date.now()
            asst.reasoning += d.text || ''
            asst.rsecs = (Date.now() - asst._r0) / 1000
            push()
          }
          else if (ev === 'delta') { asst.content += d.text || ''; push() }
          else if (ev === 'tool') { asst.tools.push(d); push() }
          else if (ev === 'error') { asst.content += '\n✗ ' + (d.error || ''); push() }
          else if (ev === 'interject') { setStatus('模型已收到插话：' + (d.text || '').slice(0, 30)) }
          else if (ev === 'done') { setSecs(d.secs); setStatus(`完成 · ${(d.secs || 0).toFixed(1)}s`) }
        }
      }
      await refresh(); if (cur) await openConv(cur)
    } catch (e) {
      if (e.name === 'AbortError') {
        setStatus('已停止')
        if (cur) { const cc = await api('/api/conv/' + cur).catch(() => null); if (cc) setMsgs(normalize(cc.messages || [])) }
      }
      else setStatus('✗ ' + e.message + '（可重试）')
    } finally { abortRef.current = null; setBusy(false); refresh() }   // 中断/报错也刷新列表，避免会话"看不见"
  }

  const act = async (kind, id, e) => {
    e?.stopPropagation()
    if (kind === 'archive') await fetch('/api/conv/' + id, { method: 'DELETE' })
    if (kind === 'restore') await fetch('/api/conv/' + id + '/restore', { method: 'POST' })
    refresh()
  }

  const list = tab === 'archive' ? archived : convs
  const cur = profiles.find(p => p.name === profile)
  // 轮次进度：本轮工具轮次 / 本会话落库的最大 seq（也即已记录的轮次）
  return (
    <div className="app">
      <div className="topbar" aria-hidden="true">
        <i style={{ width: roundMax ? Math.min(100, roundNow / roundMax * 100) + '%' : '0%' }} />
      </div>
      {showSet ? <div className="set-scrim" onClick={() => closeOne()} /> : null}

      <div className="work" style={{ '--sidew': (sideOpen ? sideW : 0) + 'px' }}>
        <aside className={'drawer' + (sideOpen ? '' : ' hide')}
          style={{ width: sideW, flexBasis: sideW }}
          >
          <span className="edge" onMouseDown={startDrag} aria-hidden="true" />
          <div className="ef-figs drawer-figs">
            <a className={tab === 'live' ? 'on' : ''} onClick={() => setTab('live')}
               title="只看会话"><FigCell value={convs.length} /><span>会话</span></a>
            <a className={tab === 'archive' ? 'on' : ''} onClick={() => setTab('archive')}
               title="只看归档"><FigCell value={archived.length} /><span>归档</span></a>
            <a title="当前会话最后一条消息的落库序号"><FigCell value={msgs.length ? (msgs[msgs.length - 1].seq || 0) : 0} /><span>本轮</span></a>
          </div>
          <div className="list" style={{ padding: '0 .5rem' }}>
            {list.length === 0 ? (
              <div style={{ padding: '.9rem .4rem', color: 'var(--rail-dim)', fontSize: '.78rem' }}>没有记录</div>
            ) : (
              <ol className="ef-records dark">
                {list.map((c, i) => (
                  <li key={c.ID}>
                    <a className={'ef-record' + (c.ID === convId ? ' on' : '')} onClick={() => openConv(c.ID)}>
                      <span className="ef-tag" title={c.ID}>No.{String(c.ID).padStart(4, '0')}</span>
                      <time className="ef-field" title="最后活跃">{fmtActive(c.UpdatedAt)}</time>
                      <span className="name ef-read">{c.Title || '(无标题)'}</span>
                        <span className="ef-field count">{c.N}</span>
                        <button className="act" title={tab === 'archive' ? '恢复' : '归档'}
                          onClick={e => { e.stopPropagation(); act(tab === 'archive' ? 'restore' : 'archive', c.ID, e) }}>
                          <svg viewBox="0 0 24 24" aria-hidden="true">
                            {/* 归档 = 箱子 + 向下箭头；恢复 = 箱子 + 向上箭头（不是垃圾桶 ✗） */}
                            <path d="M3 4h18v4H3z" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="miter" />
                            <path d="M5 8v12h14V8" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="miter" />
                            <path d={tab === 'archive' ? 'M12 16v-6m-3 3 3-3 3 3' : 'M12 10v6m-3-3 3 3 3-3'}
                              fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="square" strokeLinejoin="miter" />
                          </svg>
                        </button>
                    </a>
                  </li>
                ))}
              </ol>
            )}
          </div>
          <div className="drawer-spacer">
            <button className="new-conv" onClick={() => openConv(null)} title="新会话" aria-label="新会话">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M12 4v16M4 12h16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
              </svg>
            </button>
          </div>
          <div className="drawer-foot">
            <button className="foot-btn wide-hide" onClick={() => setShowSet(true)} title="设置">设置</button>
            <span className="foot-mark">WORKSPACE / AGENT</span>
          </div>
        </aside>
        <button className={'side-toggle' + (sideOpen ? '' : ' collapsed')}
          onClick={() => setSideOpen(!sideOpen)}
          title={sideOpen ? '折叠侧栏' : '展开侧栏'}
          aria-label={sideOpen ? '折叠侧栏' : '展开侧栏'}
          aria-expanded={sideOpen}>
          <svg className="st-icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M14 6 8 12l6 6" fill="none" stroke="currentColor" strokeWidth="1.8"
              strokeLinecap="square" strokeLinejoin="miter" />
          </svg>
        </button>

        <main className="stage" ref={stageRef} style={{ '--colw': colW + 'px' }}
          onMouseMove={onStageMove} onMouseLeave={onStageLeave} onMouseDown={onStageDown}>
          <i className="guideline" aria-hidden="true" />
          <div className="thread" ref={threadRef} onScroll={onThreadScroll}>
            {msgs.length === 0 ? (
              <div className="col empty">
                <div className="eyebrow">LOCAL AGENT CONSOLE</div>
                <h1>选一个 profile，说一句话<br /><em>剩下的交给它：调工具、读屏幕、查知识库</em></h1>
                <p className="lead">过程会留在左侧记录里；工具输出、思考链、截图都会附在回复下面。</p>
                <div className="panel ef-panel cut-tl">
                  <div className="hd"><span>START HERE</span><span>03 STEPS</span></div>
                  <div className="step">
                    <span className="n">01</span>
                    <span className="t"><b>新建会话</b><span>从空白开始，或点左侧任一历史记录继续</span></span>
                    <button onClick={() => openConv(null)}>新建</button>
                  </div>
                  <div className="step">
                    <span className="n">02</span>
                    <span className="t"><b>选 profile</b><span>180-think 规划 · 180-low 快速 · 9-exec 只跑命令</span></span>
                    <button onClick={() => document.querySelector('.composer select')?.focus()}>选择</button>
                  </div>
                  <div className="step">
                    <span className="n">03</span>
                    <span className="t"><b>写一句话</b><span>Ctrl+Enter 发送；要中断就刷新页面</span></span>
                    <button onClick={() => document.querySelector('.composer textarea')?.focus()}>去输入</button>
                  </div>
                </div>
                <div className="tips">
                  <span>会话 <code id="n1">{convs.length}</code></span>
                  <span>归档 <code>{archived.length}</code></span>
                  <span>当前 <code>{profile}</code></span>
                </div>
              </div>
            ) : (
              <div className="col">
                {msgs.map((m, i) => (
                  <div key={i} data-vidx={m.role === 'tool' ? undefined : (visible.findIndex(v => v === m) + 1)}
                      className={'msg ef-panel cut-tl ' + m.role}>
                    <div>
                      <div className="mhead">
                        <span className={'av' + (m.role === 'user' ? ' u' : m.role === 'tool' ? ' t' : '')}>{m.role === 'user' ? '用' : m.role === 'tool' ? 'T' : '◈'}</span>
                        <b>{m.role === 'user' ? '用户' : m.role === 'tool' ? (m.toolName || '工具') : 'Agent'}</b>
                        {m.interject ? <span className="ef-tag">插话</span> : null}
                        {m.seq ? <span className="ef-tag">No.{String(m.seq).padStart(3, '0')}</span> : null}
                        <span className="ef-field">{m.at || ''}</span>
                        {m.role === 'assistant' && m.content ? (
                          <span className="mact" onClick={() => navigator.clipboard?.writeText(m.content)}>复制</span>
                        ) : null}
                      </div>
                      {m.reasoning ? (
                        <details className="fold">
                          <summary>思考过程 · {String(m.reasoning).length} 字{
                            (m.reasoning_ms || (m.rsecs ? m.rsecs * 1000 : 0))
                              ? ' · ' + ((m.reasoning_ms || m.rsecs * 1000) / 1000).toFixed(1) + 's' : ''}</summary>
                          <pre className="think">{m.reasoning}</pre>
                        </details>
                      ) : null}
                      {m.content ? <Sections text={m.content} /> : null}
                      {m.role === 'tool' ? (
                        <div>
                          <div style={{ fontSize: '.72rem', color: 'var(--ink3)', marginBottom: '.2rem' }}>
                            {(m.toolName || '工具') + (m.toolArgs ? ' · ' + String(m.toolArgs).slice(0, 120) : '')}
                          </div>
                          {(m.content || '').length > 300 ? (
                            <details className="fold">
                              <summary>输出 · {String(m.content).length} 字</summary>
                              <pre className="out">{m.content}</pre>
                            </details>
                          ) : (
                            <pre className="out">{m.content}</pre>
                          )}
                        </div>
                      ) : null}
                      {(m.images || []).map((u, k) => <img key={k} src={u} style={{ maxWidth: '100%', marginTop: '.5rem', border: '1px solid var(--line)' }} />)}
                    </div>
                  </div>
                ))}
                <div ref={endRef} />
              </div>
            )}
          </div>
          <div className="status" onWheel={onComposerWheel}>{status}</div>
          <div className="composer" onWheel={onComposerWheel}>
            <div className="row1">
              <span className="eb" style={{ display: 'inline', letterSpacing: '.14em' }}>PROFILE</span>
              <label className="pick">
                <select value={profile} onChange={e => setProfile(e.target.value)}>
                  {(profiles.length ? profiles : [{ name: profile, model: '' }]).map(p => (
                    <option key={p.name} value={p.name}>{p.name}</option>
                  ))}
                </select>
                <span className="caret">▾</span>
              </label>
              <span className="model" style={{ marginLeft: 'auto' }} title={cur ? cur.model : ''}>{cur && cur.model ? cur.model : (profile === '9-exec' ? 'Qwen3.5-9B-int4-ov' : 'qwen3.8-flash-next-iq3_xxs')}</span>
            </div>
            <i className="resizer" onMouseDown={startResize} title="拖动调整输入框高度" />
            <textarea value={text} style={{ height: inputH + 'px' }} onChange={e => setText(e.target.value)}
              onKeyDown={e => {
                if (e.key !== 'Enter' || e.shiftKey || e.nativeEvent.isComposing) return   // Shift+Enter 换行 ✓
                e.preventDefault()
                if (!busy) send()
                else if (text.trim()) interject()
                else stop()
              }}
              placeholder="输入你的问题…" />
            <div className="row3">
              <span className="hint">Enter 发送 · Shift+Enter 换行　{convId ? '#' + convId.slice(0, 8) : '新会话'}</span>
              {busy ? (
                text.trim()
                  ? <button className="send interject" onClick={interject} aria-label="插话" title="插话" />
                  : <button className="send stop" onClick={stop} aria-label="中断" title="中断" />
              ) : <button className="send" onClick={send} aria-label="发送" title="发送" />}
            </div>
          </div>
          {showSet ? (
            <aside className={'settings' + ((editField || toolEdit) ? (editP ? ' three' : ' two') : (editP ? ' two' : ''))}>
              <div className="set-hd">
                <span className="eb">SETTINGS / PROFILES</span>
                {anyDirty ? (
                  <span className="card-actions">
                    <button className="btn" onClick={() => {
                      if (editorDirty) {
                        if (editField) setEditP({ ...editP, [editField]: editOriginal })
                        else setToolEdit({ ...toolEdit, description: editOriginal })
                      } else if (formDirty) {
                        setEditP(JSON.parse(editSnap || '{}'))
                      } else {
                        setSettings(JSON.parse(setSnap || '{}'))
                      }
                    }}>取消</button>
                    <button className="btn" onClick={async () => {
                      if (editorDirty) {
                        if (toolEdit) {
                          await fetch('/api/tools/' + toolEdit.name, { method: 'PUT', headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ description: toolEdit.description }) })
                          setEditOriginal(toolEdit.description || ''); refresh()
                        } else { setEditOriginal(editP?.[editField] || '') }
                        return
                      }
                      if (formDirty) {
                        if (!editP.name || !editP.base_url || !editP.model) { alert('名称 / 接口地址 / 模型 必填'); return }
                        const { _new, ...body } = editP
                        await fetch('/api/profiles', { method: 'POST', headers: { 'Content-Type': 'application/json' },
                          body: JSON.stringify({ ...body, tools: (body.tools || '').split(',').map(x => x.trim()).filter(Boolean), keep_messages: body.keep_messages || 16 }) })
                        setEditSnap(snapOf(editP)); refresh(); return
                      }
                      await fetch('/api/settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings) })
                      setSetSnap(JSON.stringify(settings))
                    }}>保存</button>
                  </span>
                ) : null}
              </div>
              {toolCfg ? (
                <div className="card col-tool">
                  <div className="card-hd">
                    <span>{'工具配置 · ' + toolCfg.name}</span>
                    <span style={{ flex: 1 }} />
                    <button className="btn" onClick={() => setToolCfg(null)}>关闭</button>
                  </div>
                  <div className="card-bd">
                    <div className="item" onClick={() => { setEditOriginal(toolCfg.description || ''); setToolEdit({ name: toolCfg.name, description: toolCfg.description }); setEditField(null) }}>
                      <span className="item-title">说明</span>
                      <span className="item-desc">{toolCfg.description || '（空）'}</span>
                    </div>
                    <div className="set-section">启用情况</div>
                    <div className="item static">
                      <span className="item-title">由 profile 决定</span>
                    </div>
                    <div className="set-meta">
                      {(() => {
                        const on = (profiles || []).filter(pr => (pr.tools || []).includes(toolCfg.name)).map(pr => pr.name)
                        return on.length ? '在 ' + on.join('、') + ' 中启用' : '当前没有任何 profile 启用它'
                      })()}
                    </div>
                    <div className="set-meta">要改可用性：打开对应模型的「工具」勾选</div>
                  </div>
                </div>
              ) : null}
              <div className={'set-cols' + ((editField || toolEdit) ? (editP ? ' three' : ' two') : (editP ? ' two' : ''))}>
                {(editField || toolEdit) ? (
                  <div className="card col-editor">
                    <div className="card-hd">{toolEdit ? ('工具说明 · ' + toolEdit.name) : '系统提示'}</div>
                    <textarea className="set-area" value={toolEdit ? (toolEdit.description || '') : (editP[editField] || '')}
                      onInput={e => autoGrow(e.target)}
                      placeholder={toolEdit ? '工具说明（模型会看到这段文字）…' : '这里写完整的系统提示…'}
                      onChange={e => {
                        if (toolEdit) setToolEdit({ ...toolEdit, description: e.target.value })
                        else setEditP({ ...editP, [editField]: e.target.value })
                      }} />
                    <div className="set-col-foot"><span className="set-meta">
                      {(toolEdit ? (toolEdit.description || '') : (editP[editField] || '')).length} 字
                    </span></div>
                  </div>
                ) : null}

                {editP ? (
                  <div className="card col-form">
                    <div className="card-hd" style={{ display: 'flex', alignItems: 'center', gap: '.6rem' }}>
                      <span>{editP._new ? '新建模型' : ('模型详情 · ' + editP.name)}</span>
                      <span style={{ flex: 1 }} />
                      {!editP._new ? (
                        <button className="btn" onClick={async () => {
                          if (!confirm('删除 ' + editP.name + '？')) return
                          await fetch('/api/profiles/' + editP.name, { method: 'DELETE' })
                          setEditP(null); setEditField(null); refresh()
                        }}>删除</button>
                      ) : null}
                    </div>
                    <div className="card-bd">
                      {[['name', '名称'], ['base_url', '接口地址 (url+/v1)'],
                        ['api_key', 'API Key']].map(([k, label]) => (                        <label className="field" key={k}><span>{label}</span>
                          <input value={editP[k] || ''} type={k === 'api_key' ? 'password' : 'text'}
                            onChange={e => setEditP({ ...editP, [k]: e.target.value })} /></label>
                      ))}
                      <label className="field"><span className="field-label">模型</span>
                        {models.loading ? <span className="item-desc">正在匹配模型…</span>
                         : models.err ? <span className="item-desc">检查接口地址或 API Key：{models.err}</span>
                         : models.list.length ? (
                          <select value={editP.model || ''} onChange={e => setEditP({ ...editP, model: e.target.value })}>
                            {models.list.map(m => <option key={m} value={m}>{m}</option>)}
                          </select>
                         ) : <input value={editP.model || ''} onChange={e => setEditP({ ...editP, model: e.target.value })} placeholder="没取到模型列表，可手填" />}
                      </label>
                      <label className="field"><span>思考强度</span>
                        <select value={editP.reasoning_effort || 'none'} onChange={e => setEditP({ ...editP, reasoning_effort: e.target.value })}>
                          {['none', 'low', 'medium', 'high'].map(x => <option key={x} value={x}>{x}</option>)}
                        </select></label>
                      <div className="field"><span>工具</span>
                        <div className="choices">
                          {toolList.map(t => {
                            const on = (editP.tools || '').split(',').map(x => x.trim()).filter(Boolean).includes(t.name)
                            return (
                              <label key={t.name} title={t.description || ''} className={'choice' + (on ? ' on' : '')}>
                                <input type="checkbox" checked={on} onChange={e => {
                                  const cur = (editP.tools || '').split(',').map(x => x.trim()).filter(Boolean)
                                  const next = e.target.checked ? [...cur, t.name] : cur.filter(x => x !== t.name)
                                  setEditP({ ...editP, tools: next.join(',') })
                                }} />
                                <span>{t.name}</span>
                              </label>
                            )
                          })}
                        </div>
                      </div>
                      <div className="item" onClick={() => { setEditOriginal(editP.system || ''); setEditField('system'); setToolEdit(null) }}>
                        <span className="item-title">系统提示</span>
                        <span className="item-desc">{editP.system || '（空）'}</span>
                      </div>
                    </div>
                  </div>
                ) : null}

                <div className="card col-list">
                  <div className="card-bd">
                    <div className="set-section">常规配置</div>
                    <div className="set-card">
                      <div className="item static">
                        <span className="item-title">工具调用轮次上限</span>
                        <button type="button" role="switch" aria-checked={!!settings.tool_rounds_enabled}
                          className={'tg' + (settings.tool_rounds_enabled ? ' on' : '')}
                          onClick={() => setSettings({ ...settings, tool_rounds_enabled: !settings.tool_rounds_enabled })}><b /></button>
                      </div>
                      <div className="set-meta">开启该项以避免工具循环调用</div>
                      {settings.tool_rounds_enabled ? (
                        <label className="field" style={{ marginTop: '.5rem' }}><span>轮次上限</span>
                          <input type="number" min="1" max="1000" value={settings.tool_rounds || 8}
                            onChange={e => setSettings({ ...settings, tool_rounds: Number(e.target.value) || 8 })} /></label>
                      ) : null}
                    </div>

                    <div className="set-card">
                      <div className="item static">
                        <span className="item-title">自动归档</span>
                        <button type="button" role="switch" aria-checked={!!settings.auto_archive_enabled}
                          className={'tg' + (settings.auto_archive_enabled ? ' on' : '')}
                          onClick={() => setSettings({ ...settings, auto_archive_enabled: !settings.auto_archive_enabled })}><b /></button>
                      </div>
                      <div className="set-meta">超过设定天数未活跃的会话自动归入归档</div>
                      {settings.auto_archive_enabled ? (
                        <label className="field" style={{ marginTop: '.5rem' }}><span>未活跃天数</span>
                          <input type="number" min="1" max="3650" value={settings.auto_archive_days || 7}
                            onChange={e => setSettings({ ...settings, auto_archive_days: Number(e.target.value) || 7 })} /></label>
                      ) : null}
                    </div>

                    <div className="set-section">模型配置</div>
                    <div className="set-card">
                      {profiles.map(pr => (
                        <div className={'item col' + (editP && editP.name === pr.name ? ' on' : '')} key={pr.name}
                          onClick={() => { const v = { ...pr, tools: (pr.tools || []).join(',') }; setEditP(v); setEditSnap(snapOf(v)); setEditField(null); setToolEdit(null) }}>
                          <b className="item-title">{pr.name}</b>
                          <span className="item-desc">{pr.model || '（未设置模型）'}</span>
                        </div>
                      ))}
                      <div className="item item-new" onClick={() => {
                        const v = { _new: true, name: '', base_url: 'http://127.0.0.1:18320/180/v1', api_key: '',
                          model: 'qwen3.8-flash-next-iq3_xxs', reasoning_effort: 'high', tools: '', system: '' }
                        setEditP(v); setEditSnap(''); setEditField(null); setToolEdit(null)
                      }}>＋ 新建 profile</div>
                    </div>

                    <div className="set-section">工具配置</div>
                    <div className="set-card">
                      {toolList.map(t => (
                        <div className="item" key={t.name}
                          onClick={() => { setToolCfg({ ...t, enabled: t.enabled !== false, subtools: t.subtools || [] }); setToolEdit(null); setEditField(null) }}>
                          <b className="item-title">{t.name}</b>
                          <span className="item-desc">{t.description}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            </aside>
          ) : null}
        </main>
        {peek.open && peekRows.length ? (
          <div className="peek" style={{ top: peek.top || '50%' }}
            onMouseEnter={() => setPeek(p => ({ ...p, open: true }))}
            onMouseLeave={() => setPeek({ open: false, idx: -1, top: 0 })}
            onWheel={e => {
              e.preventDefault()
              const base = peek.idx >= 0 ? peek.idx : curIdx()
              const next = Math.max(0, Math.min(visible.length - 1, base + (e.deltaY > 0 ? 1 : -1)))
              if (next !== base) {
                setPeek(p => ({ ...p, idx: next }))
                scrollToMsg(next)          // 同时滚动页面 → 分子随之更新 ✓
              }
            }}>
            {peekRows.map(({ i, m }) => (
              <button key={i}
                className={'peek-row' + ((peek.idx < 0 ? curIdx() : peek.idx) === i ? ' on' : '')}
                onClick={() => { scrollToMsg(i); setPeek({ open: false, idx: -1 }) }}
                onMouseEnter={() => setPeek(p => ({ ...p, open: true, idx: i }))}>
                <span className="peek-no">{String(i + 1).padStart(3, '0')}</span>
                <span className="peek-tx">{String(m.content || m.role || '').replace(/\s+/g, ' ').slice(0, 18)}</span>
              </button>
            ))}
          </div>
        ) : null}
        <aside className="rail">
          <span className="vt">AGENT EXECUTE · CREATE</span>          <span className="rail-mid">
            <span className="fig" style={{ cursor: 'pointer' }}
              onMouseEnter={e => { const r = e.currentTarget.getBoundingClientRect(); setPeek({ open: true, idx: curIdx(), top: r.top + r.height / 2 }) }}
              onMouseLeave={() => setPeek({ open: false, idx: -1, top: 0 })}
              onWheel={e => {
                e.preventDefault()
                const base = peek.idx >= 0 ? peek.idx : curIdx()
                const next = Math.max(0, Math.min(visible.length - 1, base + (e.deltaY > 0 ? 1 : -1)))
                if (next !== base) {
                  setPeek(p => ({ ...p, idx: next }))
                  scrollToMsg(next)        // 滚动页面 ✓
                }
              }}>
              {String(roundNow).padStart(2,'0')} / {roundMax ? String(roundMax).padStart(2,'0') : '∞'}
            </span>
            <span className="rail-status"><span className={'dot' + (busy ? ' busy' : '')} />{busy ? '运行中' : '空闲'}</span>
          </span>
          <div className="rail-acts">
            <button className="dots" onClick={() => openConv(null)} aria-label="新会话" title="新会话">
              <svg className="plus" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M12 4v16M4 12h16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="square"/>
              </svg>
            </button>
            <button className="dots" onClick={() => setShowSet(true)} aria-label="设置" title="设置">
              <i /><i /><i />
            </button>
          </div>
        </aside>
      </div>
    </div>
  )
}
