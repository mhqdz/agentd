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

// 把回复按 markdown 标题切成"01 / 标题"编号段落；无标题则整段输出
function Sections({ text }) {
  const lines = String(text || '').split('\n')
  const out = []
  let buf = [], n = 0
  const flush = () => {
    if (buf.length) {
      const body = buf.join('\n').trim()
      if (body) out.push(<pre className="prose" key={'b' + out.length}>{body}</pre>)
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
  if (out.length === 0) return <pre className="prose">{text}</pre>
  return <>{out}</>
}

export default function App() {
  const [convs, setConvs] = useState([])
  const [archived, setArchived] = useState([])
  const [profiles, setProfiles] = useState([])
  const [profile, setProfile] = useState(localStorage.getItem('profile') || '180-think')
  const [tab, setTab] = useState('live')
  const [sideOpen, setSideOpen] = useState(localStorage.getItem('sideOpen') !== '0')
  const [convId, setConvId] = useState(null)
  const [title, setTitle] = useState('新会话')
  const [msgs, setMsgs] = useState([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState('')
  const [secs, setSecs] = useState(null)
  const endRef = useRef(null)

  const refresh = async () => {
    setConvs(await api('/api/convs'))
    setArchived(await api('/api/convs?archived=1').catch(() => []))
    setProfiles(await api('/api/profiles'))
  }
  useEffect(() => { refresh() }, [])
  useEffect(() => { localStorage.setItem('profile', profile) }, [profile])
  useEffect(() => { localStorage.setItem('sideOpen', sideOpen ? '1' : '0') }, [sideOpen])
  useEffect(() => { endRef.current?.scrollIntoView({ block: 'end' }) }, [msgs, status])

  const openConv = async (id) => {
    setConvId(id)
    if (!id) { setMsgs([]); setTitle('新会话'); return }
    const c = await api('/api/conv/' + id)
    setMsgs(c.messages || []); setTitle(c.title || '会话')
  }

  const send = async () => {
    const t = text.trim(); if (!t || busy) return
    setText(''); setBusy(true); setStatus(''); setSecs(null)
    setMsgs(m => [...m, { role: 'user', content: t, at: now() }])
    let cur = convId
    try {
      const resp = await fetch('/api/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conv_id: cur || '', text: t, profile }),
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
          else if (ev === 'done') { setSecs(d.secs); setStatus(`完成 · ${(d.secs || 0).toFixed(1)}s`) }
        }
      }
      await refresh(); if (cur) await openConv(cur)
    } catch (e) { setStatus('✗ ' + e.message) } finally { setBusy(false) }
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
        <button onClick={() => openConv(null)}>＋ 新建</button>
      </header>

      <div className="work">
        <aside className={'drawer' + (sideOpen ? '' : ' hide')}>
          <div className="tabs">
            <a className={tab === 'live' ? 'on' : ''} onClick={() => setTab('live')}>会话 {convs.length}</a>
            <a className={tab === 'archive' ? 'on' : ''} onClick={() => setTab('archive')}>归档 {archived.length}</a>
          </div>
          <div className="list">
            {list.length === 0 ? <div style={{ padding: '.9rem', color: '#6f8582', fontSize: '.78rem' }}>没有记录</div> : null}
            {list.map((c, i) => (
              <div key={c.ID} className={'r' + (c.ID === convId ? ' on' : '')} onClick={() => openConv(c.ID)}>
                <span className="no">{pad(i + 1)}</span>
                <span className="dt ef-field">{(c.UpdatedAt || '').slice(5, 10).replace('-', '.')}</span>
                <span className="ti">{c.Title || '(无标题)'}</span>
                <span className="ct ef-field">{String(c.N || 0).padStart(2, '0')}</span>
                <span className="act" onClick={e => act(tab === 'archive' ? 'restore' : 'archive', c.ID, e)}>
                  {tab === 'archive' ? '恢复' : '归档'}
                </span>
              </div>
            ))}
          </div>
          <div className="foot">WORKSPACE / AGENT</div>
        </aside>

        <main className="stage">
          <div className="thread">
            {msgs.length === 0 ? (
              <div className="col empty">
                <div className="eyebrow">LOCAL AGENT CONSOLE</div>
                <h1>选一个 profile，说一句话<br /><em>剩下的交给它：调工具、读屏幕、查知识库</em></h1>
                <p className="lead">过程会留在左侧记录里；工具输出、思考链、截图都会附在回复下面。</p>
                <div className="panel">
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
                  <div key={i} className={'msg ' + m.role}>
                    <div className="av">{m.role === 'user' ? '用' : m.role === 'tool' ? 'T' : '◈'}</div>
                    <div>
                      <div className="mhead">
                        <b>{m.role === 'user' ? '用户' : m.role === 'tool' ? '工具' : 'Agent'}</b>
                        <span className="ef-field">{m.at || ''}</span>
                        {m.seq ? <span className="ef-field">#{m.seq}</span> : null}
                      </div>
                      {m.reasoning ? (
                        <details className="fold">
                          <summary>思考过程 · {String(m.reasoning).length} 字</summary>
                          <pre className="think">{m.reasoning}</pre>
                        </details>
                      ) : null}
                      {m.content ? <Sections text={m.content} /> : null}
                      {(m.tools || []).length > 0 ? (
                        <div className="tools">
                          <div className="hd"><span>TOOL LOG / 工具调用记录</span><span>{m.tools.length} 项 · 全部完成</span></div>
                          {m.tools.map((t, k) => (
                            <div className="row" key={k}>
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
            <textarea value={text} onChange={e => setText(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}
              placeholder="输入你的问题…" />
            <div className="row3">
              <span className="hint">Ctrl+Enter 发送　{convId ? '#' + convId.slice(0, 8) : '新会话'}</span>
              <button className="send" onClick={send} disabled={busy}>{busy ? '运行中' : '发送 ↗'}</button>
            </div>
          </div>
        </main>
        <aside className="rail">
          <span className="vt">AGENT EXECUTE · CREATE</span>
          <span className="fig">{String(convs.length).padStart(2,'0')} / {String(archived.length).padStart(2,'0')}</span>
          <span className="dots"><i /><i /><i /></span>
        </aside>
      </div>
    </div>
  )
}
