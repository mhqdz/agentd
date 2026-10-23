import React, { useEffect, useRef, useState } from 'react'

const api = (p, opt) => fetch(p, opt).then(r => r.json())
const pad = n => String(n).padStart(2, '0')

export default function App() {
  const [convs, setConvs] = useState([])
  const [archived, setArchived] = useState([])
  const [profiles, setProfiles] = useState([])
  const [profile, setProfile] = useState(localStorage.getItem('profile') || '180-think')
  const [showArchived, setShowArchived] = useState(false)
  const [convId, setConvId] = useState(null)
  const [title, setTitle] = useState('新会话')
  const [msgs, setMsgs] = useState([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState('')
  const endRef = useRef(null)

  const refresh = async () => {
    setConvs(await api('/api/convs'))
    setArchived(await api('/api/convs?archived=1').catch(() => []))
    setProfiles(await api('/api/profiles'))
  }
  useEffect(() => { refresh() }, [])
  useEffect(() => { localStorage.setItem('profile', profile) }, [profile])
  useEffect(() => { endRef.current?.scrollIntoView({ block: 'end' }) }, [msgs, status])

  const openConv = async (id) => {
    setConvId(id)
    if (!id) { setMsgs([]); setTitle('新会话'); return }
    const c = await api('/api/conv/' + id)
    setMsgs(c.messages || [])
    setTitle(c.title || '会话')
  }

  const send = async () => {
    const t = text.trim(); if (!t || busy) return
    setText(''); setBusy(true); setStatus('')
    setMsgs(m => [...m, { role: 'user', content: t, at: new Date() }])
    let cur = convId
    try {
      const resp = await fetch('/api/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conv_id: cur || '', text: t, profile }),
      })
      const reader = resp.body.getReader(); const dec = new TextDecoder()
      let buf = '', asst = { role: 'assistant', content: '', reasoning: '', tools: [], at: new Date() }
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
          else if (ev === 'tool') { asst.tools.push({ name: d.name, args: d.args, text: d.text, image: d.image }); push() }
          else if (ev === 'error') { asst.content += '\n✗ ' + (d.error || ''); push() }
          else if (ev === 'done') { setStatus(`已完成分析 · ${(d.secs || 0).toFixed(1)}s`) }
        }
      }
      await refresh(); if (cur) await openConv(cur)
    } catch (e) { setStatus('✗ ' + e.message) } finally { setBusy(false) }
  }

  const list = showArchived ? archived : convs

  return (
    <div className="app">
      <aside className="side">
        <div className="brand">
          <svg width="26" height="26" viewBox="0 0 26 26"><rect x="1" y="1" width="24" height="24" fill="none" stroke="#8fae9f"/><path d="M6 20 L13 6 L20 20" fill="none" stroke="#e6ecea"/><path d="M9 16 H17" stroke="#8fae9f"/></svg>
          <div><b>Agent</b><small>EXECUTE · CREATE</small></div>
        </div>
        <nav className="nav">
          <a className={!showArchived ? 'on' : ''} onClick={() => { setShowArchived(false); openConv(null) }}>
            <span>对话</span><span>＋</span></a>
          <a className={showArchived ? 'on' : ''} onClick={() => setShowArchived(true)}>
            <span>归档</span><span>{archived.length}</span></a>
        </nav>
        <div className="grp">{showArchived ? 'ARCHIVE / 归档' : 'HISTORY / 历史会话'} · {list.length}</div>
        <div className="recs">
          {list.length === 0 ? <div className="empty" style={{ color: 'var(--side-dim)' }}>没有记录</div> : null}
          {list.map((c, i) => (
            <div key={c.ID} className={'rec' + (c.ID === convId ? ' on' : '')} onClick={() => openConv(c.ID)}>
              <span className="no">{pad(i + 1)}</span>
              <span className="ti">{c.Title || '(无标题)'}</span>
              {showArchived ? (
                <span className="ef-btn" style={{ padding: '0 .35rem', fontSize: '.7em' }}
                  onClick={async (e) => { e.stopPropagation(); await fetch('/api/conv/' + c.ID + '/restore', { method: 'POST' }); refresh() }}>恢复</span>
              ) : (
                <span className="ef-btn" style={{ padding: '0 .35rem', fontSize: '.7em' }}
                  onClick={async (e) => { e.stopPropagation(); await fetch('/api/conv/' + c.ID, { method: 'DELETE' }); refresh() }}>归档</span>
              )}
            </div>
          ))}
        </div>
        <div className="grp" style={{ borderTop: '1px solid #23302f' }}>WORKSPACE / AGENT</div>
      </aside>

      <main className="main">
        <header className="top">
          <button className="ef-btn" onClick={() => openConv(null)}>＋ 新建</button>
          <h2>{title}</h2>
          <span className="chip">工作区</span>
          <span className="spacer" />
          <span style={{ color: 'var(--ink2)', fontSize: '.8rem' }}><span className="dot" />{busy ? '运行中' : '空闲'}</span>
          <select className="sel" value={profile} onChange={e => setProfile(e.target.value)}>
            {profiles.map(p => <option key={p.name} value={p.name}>{p.name} · {p.model}</option>)}
          </select>

          <span className="brandline">AGENT EXECUTE · CREATE</span>
        </header>

        <div className="thread">
          {msgs.map((m, i) => (
            <div key={i} className={'msg ' + m.role}>
              <div className="av">{m.role === 'user' ? '用' : '◈'}</div>
              <div>
                <div className="who">
                  <b>{m.role === 'user' ? '用户' : (m.role === 'tool' ? '工具' : 'Agent')}</b>
                  <span>{new Date(m.at || Date.now()).toTimeString().slice(0, 5)}</span>
                  {m.seq ? <span>#{m.seq}</span> : null}
                </div>
                {m.reasoning ? <div className="think">{m.reasoning}</div> : null}
                {m.content ? <div className="prose">{m.content}</div> : null}
                {(m.tools || []).length > 0 ? (
                  <div className="tools">
                    <div className="hd"><span>工具调用记录</span><span>{m.tools.length} 项</span></div>
                    {m.tools.map((t, k) => (
                      <div className="row" key={k}>
                        <span className="nm">{t.name}</span>
                        <span>
                          <span style={{ color: 'var(--ink2)' }}>{(t.args || '').slice(0, 120)}</span>
                          {t.text ? <pre>{t.text}</pre> : null}
                          {t.image ? <img src={t.image} alt="tool" /> : null}
                        </span>
                        <span style={{ color: '#4f9d7a' }}>✓</span>
                      </div>
                    ))}
                  </div>
                ) : null}
                {(m.images || []).map((u, k) => <img key={k} src={u} style={{ maxWidth: '100%', marginTop: '.5rem' }} />)}
              </div>
            </div>
          ))}
          <div ref={endRef} />
        </div>
        <div className="status">{status}</div>
        <div className="composer">
          <select className="sel" value={profile} onChange={e => setProfile(e.target.value)}>
            {profiles.map(p => <option key={p.name} value={p.name}>{p.name}</option>)}
          </select>
          <textarea value={text} onChange={e => setText(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}
            placeholder="输入你的问题，Ctrl+Enter 发送" />
          <button className="send" onClick={send} disabled={busy}>↗</button>
        </div>
      </main>
    </div>
  )
}
