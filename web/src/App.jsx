import React, { useEffect, useRef, useState } from 'react'

const api = (p, opt) => fetch(p, opt).then(r => r.json())
const pad = n => String(n).padStart(2, '0')
const now = () => new Date().toTimeString().slice(0, 5)



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

function EditorCol({ value, onChange, onDone, placeholder }) {
  return (
    <div className="set-col col-editor">
      <textarea className="set-area" value={value || ''} placeholder={placeholder}
        onChange={e => onChange(e.target.value)} />
      <div className="set-col-foot">
        <span className="set-meta" style={{ marginRight: 'auto' }}>{(value || '').length} 字</span>
        <button className="ef-btn small" onClick={onDone}>完成</button>
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
      if (body) out.push(<pre className="prose ef-prose" key={'b' + out.length}>{body}</pre>)
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
  if (out.length === 0) return <pre className="prose ef-prose">{text}</pre>
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
  const threadRef = useRef(null)
  const [inputH, setInputH] = useState(() => Number(localStorage.getItem('inputH')) || 92)
  const hDrag = useRef(null)
  const abortRef = useRef(null)
  const [aborted, setAborted] = useState(false)
  const [showSet, setShowSet] = useState(new URLSearchParams(location.search).has('settings'))
  const [editP, setEditP] = useState(null)
  const [toolList, setToolList] = useState([])
  const [toolEdit, setToolEdit] = useState(null)
  const [editField, setEditField] = useState(null)
  const drag = useRef(null)
  const [convId, setConvId] = useState(null)
  const [title, setTitle] = useState('新会话')
  const [msgs, setMsgs] = useState([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
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
    abortRef.current?.abort()
    abortRef.current = null
    setAborted(true)
    setStatus('已停止（已保留已生成的内容）')
  }

  // 滚轮：在输入区/状态栏上滚动也归给会话区（否则滚轮在这里"失灵"）
  const onStageWheel = (e) => {
    const th = stageRef.current?.querySelector('.thread')
    if (!th) return
    if (e.target.closest('.thread')) return          // 会话区内部走原生滚动
    th.scrollTop += e.deltaY                          // 输入区上滚 → 滚动会话
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

  const refresh = async () => {
    setConvs(await api('/api/convs'))
    setArchived(await api('/api/convs?archived=1').catch(() => []))
    setProfiles(await api('/api/profiles'))
    setToolList(await api('/api/tools').catch(() => []))
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
  useEffect(() => { localStorage.setItem('profile', profile) }, [profile])
  useEffect(() => { localStorage.setItem('sideOpen', sideOpen ? '1' : '0') }, [sideOpen])
  // 自动跟随：仅当用户已在底部附近才滚到底；往上翻阅读历史时不打断
  useEffect(() => {
    const th = threadRef.current
    if (!th) return
    const nearBottom = th.scrollHeight - th.scrollTop - th.clientHeight < 120
    if (nearBottom) th.scrollTop = th.scrollHeight
  }, [msgs, status])

  const openConv = async (id) => {
    setConvId(id)
    if (!id) { setMsgs([]); setTitle('新会话'); return }
    const c = await api('/api/conv/' + id)
    setMsgs(c.messages || []); setTitle(c.title || '会话')
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
          if (ev === 'meta') { cur = d.conv_id; setConvId(cur); if (d.title) setTitle(d.title) }
          else if (ev === 'reasoning') { asst.reasoning += d.text || ''; push() }
          else if (ev === 'delta') { asst.content += d.text || ''; push() }
          else if (ev === 'tool') { asst.tools.push(d); push() }
          else if (ev === 'error') { asst.content += '\n✗ ' + (d.error || ''); push() }
          else if (ev === 'interject') { setStatus('模型已收到插话：' + (d.text || '').slice(0, 30)) }
          else if (ev === 'done') { setSecs(d.secs); setStatus(`完成 · ${(d.secs || 0).toFixed(1)}s`) }
        }
      }
      await refresh(); if (cur) await openConv(cur)
    } catch (e) {
      if (e.name === 'AbortError') setStatus('已停止')
      else setStatus('✗ ' + e.message + '（可重试）')
    } finally { abortRef.current = null; setBusy(false) }
  }

  const act = async (kind, id, e) => {
    e?.stopPropagation()
    if (kind === 'archive') await fetch('/api/conv/' + id, { method: 'DELETE' })
    if (kind === 'restore') await fetch('/api/conv/' + id + '/restore', { method: 'POST' })
    refresh()
  }

  const list = tab === 'archive' ? archived : convs
  const cur = profiles.find(p => p.name === profile)
  return (
    <div className="app">
      <header className="cmd">
        <button onClick={() => setSideOpen(!sideOpen)} title="折叠/展开侧栏">{sideOpen ? '«' : '»'}</button>
        <div className="brand">
          <span className="seal">A</span>
          <div><b>Agent</b><small>EXECUTE · CREATE</small></div>
        </div>
        <div className="figs">
          <div className="fig"><b>{convs.length}</b><span>SESSIONS</span></div>
          <div className="fig"><b>{archived.length}</b><span>ARCHIVE</span></div>
          <div className="fig"><b>{(cur?.reasoning_effort || '—').toUpperCase()}</b><span>EFFORT</span></div>
          <div className="fig"><b>{secs ? secs.toFixed(1) + 's' : '—'}</b><span>LAST RUN</span></div>
        </div>
        <div className="grow" />
        <span className="status" style={{ fontSize: '.75rem', color: '#9fb3b0' }}><span className={'dot' + (busy ? ' busy' : '')} />{busy ? '运行中' : '空闲'}</span>
        <button onClick={() => setShowSet(true)}>设置</button>
        <button onClick={() => openConv(null)}>＋ 新建</button>
      </header>

      <div className="work">
        <aside className={'drawer' + (sideOpen ? '' : ' hide')}
          style={{ width: sideW, flexBasis: sideW }}>
          <div className="ef-figs drawer-figs">
            <a className={tab === 'live' ? 'on' : ''} onClick={() => setTab('live')}
               title="只看会话"><b>{String(convs.length).padStart(2, '0')}</b><span>会话</span></a>
            <a className={tab === 'archive' ? 'on' : ''} onClick={() => setTab('archive')}
               title="只看归档"><b>{String(archived.length).padStart(2, '0')}</b><span>归档</span></a>
            <a title="当前会话消息数"><b>{String(msgs.length).padStart(2, '0')}</b><span>本轮</span></a>
          </div>
          <div className="list" style={{ padding: '0 .5rem' }}>
            {list.length === 0 ? (
              <div style={{ padding: '.9rem .4rem', color: 'var(--rail-dim)', fontSize: '.78rem' }}>没有记录</div>
            ) : (
              <ol className="ef-records dark">
                {list.map((c, i) => (
                  <li key={c.ID}>
                    <a className={'ef-record' + (c.ID === convId ? ' on' : '')} onClick={() => openConv(c.ID)}>
                      <span className="ef-tag">No.{pad(i + 1)}</span>
                      <time className="ef-field">{(c.UpdatedAt || '').slice(5, 10).replace('-', '.')}</time>
                      <span className="name ef-read">{c.Title || '(无标题)'}</span>
                      <span className="ef-field">{c.N}</span>
                    </a>
                    <span className="act" onClick={e => act(tab === 'archive' ? 'restore' : 'archive', c.ID, e)}>
                      {tab === 'archive' ? '恢复' : '归档'}
                    </span>
                  </li>
                ))}
              </ol>
            )}
          </div>
          <div className="foot">WORKSPACE / AGENT</div>
        </aside>
        {sideOpen ? <div className="split" onMouseDown={startDrag} title="拖动调整左侧面板宽度" /> : null}

        <main className="stage" ref={stageRef} style={{ '--colw': colW + 'px' }}
          onMouseMove={onStageMove} onMouseLeave={onStageLeave} onMouseDown={onStageDown}
          onWheel={onStageWheel}>
          <i className="guideline" aria-hidden="true" />
          <div className="thread" ref={threadRef}>
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
                  <div key={i} className={'msg ef-panel cut-tl ' + m.role}>
                    <div>
                      <div className="mhead">
                        <span className={'av' + (m.role === 'user' ? ' u' : m.role === 'tool' ? ' t' : '')}>{m.role === 'user' ? '用' : m.role === 'tool' ? 'T' : '◈'}</span>
                        <b>{m.role === 'user' ? '用户' : m.role === 'tool' ? '工具' : 'Agent'}</b>
                        {m.interject ? <span className="ef-tag">插话</span> : null}
                        {m.seq ? <span className="ef-tag">No.{String(m.seq).padStart(3, '0')}</span> : null}
                        <span className="ef-field">{m.at || ''}</span>
                        {m.role === 'assistant' && m.content ? (
                          <span className="mact" onClick={() => navigator.clipboard?.writeText(m.content)}>复制</span>
                        ) : null}
                      </div>
                      {m.reasoning ? (
                        <details className="fold">
                          <summary>思考过程 · {String(m.reasoning).length} 字</summary>
                          <pre className="think">{m.reasoning}</pre>
                        </details>
                      ) : null}
                      {m.content ? <Sections text={m.content} /> : null}
                      {(m.tools || []).length > 0 ? (
                        <div className="tools ef-panel cut-tl">
                          <div className="hd"><span>TOOL LOG / 工具调用记录</span><span>{m.tools.length} 项 · 全部完成</span></div>
                          {m.tools.map((t, k) => (
                            <div className="row ef-row" key={k}>
                              <span className="nm">{t.name}</span>
                              <div>
                                <div style={{ color: 'var(--ink2)', fontSize: '.78rem' }}>{(t.args || '').slice(0, 140)}</div>
                                {t.text ? <details className="fold"><summary>输出 · {String(t.text).length} 字</summary><pre className="out">{t.text}</pre></details> : null}
                                {t.image ? <img src={t.image} alt="tool" /> : null}
                              </div>
                              <span className="ok">✓</span>
                            </div>
                          ))}
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
          <div className="status">{status}</div>
          <div className="composer">
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
              onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}
              placeholder="输入你的问题…" />
            <div className="row3">
              <span className="hint">Ctrl+Enter 发送　{convId ? '#' + convId.slice(0, 8) : '新会话'}</span>
              {busy ? (
                <>
                  <button className="send" onClick={interject} disabled={!text.trim()}>插话 ↵</button>
                  <button className="send stop" onClick={stop}>停止 ■</button>
                </>
              ) : <button className="send" onClick={send}>发送 ↗</button>}
            </div>
          </div>
          {showSet ? (
            <aside className={'settings' + ((editField || toolEdit) ? (editP ? ' three' : ' two') : (editP ? ' two' : ''))}>
              <div className="set-hd">
                <span className="eb">SETTINGS / PROFILES</span>
                <button className="ef-btn small" onClick={() => { setShowSet(false); setEditP(null); setEditField(null) }}>关闭 ✕</button>
              </div>
              <div className={'set-cols' + ((editField || toolEdit) ? (editP ? ' three' : ' two') : (editP ? ' two' : ''))}>
                {(editField || toolEdit) ? (
                  toolEdit ? (
                    <EditorCol value={toolEdit.description} placeholder="工具说明（模型会看到这段文字）…"
                      onChange={v => setToolEdit({ ...toolEdit, description: v })}
                      onDone={async () => {
                        await fetch('/api/tools/' + toolEdit.name, { method: 'PUT', headers: { 'Content-Type': 'application/json' },
                          body: JSON.stringify({ description: toolEdit.description }) })
                        setToolEdit(null); refresh()
                      }} />
                  ) : (
                    <EditorCol value={editP[editField]} placeholder="这里写完整的系统提示…"
                      onChange={v => setEditP({ ...editP, [editField]: v })}
                      onDone={() => setEditField(null)} />
                  )
                ) : null}

                {editP ? (
                  <div className="set-col col-form">
                    <div className="set-col-scroll">
                      {[['name', '名称（英文短名）'], ['base_url', '接口地址（本地或云端 /v1）'],
                        ['api_key', 'API Key（云端必填，本地可空）'], ['model', '模型名']].map(([k, label]) => (
                        <label className="set-f" key={k}><span>{label}</span>
                          <input value={editP[k] || ''} type={k === 'api_key' ? 'password' : 'text'}
                            onChange={e => setEditP({ ...editP, [k]: e.target.value })} /></label>
                      ))}
                      <label className="set-f"><span>思考强度</span>
                        <select value={editP.reasoning_effort || 'none'} onChange={e => setEditP({ ...editP, reasoning_effort: e.target.value })}>
                          {['none', 'low', 'medium', 'high'].map(x => <option key={x} value={x}>{x}</option>)}
                        </select></label>
                      <div className="set-f"><span>工具（勾选启用）</span>
                        <div className="tool-list">
                          {(toolList.length ? toolList : [{ name: 'bash', description: '' }, { name: 'screenshot', description: '' }, { name: 'kb_search', description: '' }, { name: 'recall', description: '' }]).map(t => {
                            const on = (editP.tools || '').split(',').map(x => x.trim()).filter(Boolean).includes(t.name)
                            return (
                              <label key={t.name} title={t.description || ''} className={'tool-item' + (on ? ' on' : '')}>
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
                      <div className="set-f"><span>系统提示</span></div>
                      <div className="set-row" style={{ alignItems: 'flex-start', marginTop: '.2rem' }}>
                        <div className="prompt-text" title="双击编辑"
                          onDoubleClick={() => setEditField(editField === 'system' ? null : 'system')}>
                          {editP.system || '（空）'}
                        </div>
                      </div>
                    </div>
                    <div className="set-col-foot">
                      <button className="ef-btn small" onClick={async () => {
                        if (!editP.name || !editP.base_url || !editP.model) { alert('名称 / 接口地址 / 模型 必填'); return }
                        const { _new, ...body } = editP
                        await fetch('/api/profiles', { method: 'POST', headers: { 'Content-Type': 'application/json' },
                          body: JSON.stringify({ ...body, tools: (body.tools || '').split(',').map(x => x.trim()).filter(Boolean), keep_messages: body.keep_messages || 16 }) })
                        setEditP(null); setEditField(null); refresh()
                      }}>保存</button>
                      {!editP._new && !editP.builtin ? (
                        <button className="ef-btn small" onClick={async () => {
                          if (!confirm('删除 ' + editP.name + '？')) return
                          await fetch('/api/profiles/' + editP.name, { method: 'DELETE' })
                          setEditP(null); setEditField(null); refresh()
                        }}>删除</button>
                      ) : null}
                      <button className="ef-btn small" onClick={() => { setEditP(null); setEditField(null) }}>取消</button>
                    </div>
                  </div>
                ) : null}

                <div className="set-col col-list">
                  <div className="set-col-scroll">
                    <div className="set-section">模型配置</div>
                    {profiles.map(pr => (
                      <div className={'set-card set-card-click' + (editP && editP.name === pr.name ? ' on' : '')} key={pr.name}
                        onClick={() => { setEditP({ ...pr, tools: (pr.tools || []).join(',') }); setEditField(null); setToolEdit(null) }}>
                        <div className="set-row">
                          <b className="set-name" title="单击编辑"
                            onClick={() => { setEditP({ ...pr, tools: (pr.tools || []).join(',') }); setEditField(null) }}>{pr.name}</b>
                        </div>
                        <div className="set-meta">{pr.model || '（未设置模型）'}</div>
                      </div>
                    ))}
                    <div className="set-section">工具配置</div>
                    <div className="set-card">
                      {toolList.map(t => (
                        <div className="tool-desc" key={t.name}>
                          <b>{t.name}</b>
                          <span title="双击编辑说明" style={{ cursor: 'text' }}
                            onDoubleClick={() => { setToolEdit({ name: t.name, description: t.description }); setEditField(null) }}>{t.description}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                  <div className="set-col-foot">
                    <button className="ef-btn small" onClick={() => { setEditP({
                      _new: true, name: '', base_url: 'http://127.0.0.1:18320/180/v1', api_key: '',
                      model: 'qwen3.8-flash-next-iq3_xxs', reasoning_effort: 'high', tools: '', system: ''
                    }); setEditField(null) }}>＋ 新建 profile</button>
                  </div>
                </div>
              </div>
            </aside>
          ) : null}
        </main>
        <aside className="rail">
          <span className="vt">AGENT EXECUTE · CREATE</span>
          <span className="fig">{String(convs.length).padStart(2,'0')} / {String(archived.length).padStart(2,'0')}</span>
          <button className="dots" onClick={() => setShowSet(true)}>
            <i /><i /><i />
          </button>
        </aside>
      </div>
    </div>
  )
}
