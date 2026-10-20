import React, { useEffect, useRef, useState } from 'react'

const api = (p, opt) => fetch(p, opt).then(r => r.json())

function ToolCard({ t }) {
  return (
    <details className="tool" open={false}>
      <summary>{t.name} <span style={{ color: '#6b7385' }}>{t.args?.slice(0, 90)}</span></summary>
      {t.text ? <pre>{t.text}</pre> : null}
      {t.image ? <div style={{ color: '#8b93a3', fontSize: 12, marginTop: 6 }}>（含截图，见对话末尾的图片）</div> : null}
    </details>
  )
}

export default function App() {
  const [convs, setConvs] = useState([])
  const [archived, setArchived] = useState([])
  const [profiles, setProfiles] = useState([])
  const [profile, setProfile] = useState(localStorage.getItem('profile') || '180-think')
  const [showArchived, setShowArchived] = useState(false)
  const [convId, setConvId] = useState(null)
  const [msgs, setMsgs] = useState([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState('')
  const boxRef = useRef(null)

  const refresh = async () => {
    setConvs(await api('/api/convs'))
    setArchived(await api('/api/convs?archived=1').catch(() => []))
    setProfiles(await api('/api/profiles'))
  }
  useEffect(() => { refresh() }, [])
  useEffect(() => { localStorage.setItem('profile', profile) }, [profile])
  useEffect(() => { const b = boxRef.current; if (b) b.scrollTop = b.scrollHeight }, [msgs, status])

  const openConv = async (id) => {
    setConvId(id)
    if (!id) { setMsgs([]); return }
    const c = await api('/api/conv/' + id)
    setMsgs(c.messages || [])
  }

  const send = async () => {
    const t = text.trim(); if (!t || busy) return
    setText(''); setBusy(true); setStatus('')
    setMsgs(m => [...m, { role: 'user', content: t }])
    let cur = convId
    try {
      const resp = await fetch('/api/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conv_id: cur || '', text: t, profile }),
      })
      const reader = resp.body.getReader(); const dec = new TextDecoder()
      let buf = '', asst = { role: 'assistant', content: '', reasoning: '' }, tools = []
      const push = () => setMsgs(m => {
        const base = m.filter(x => !x.__live)
        return [...base, { ...asst, __live: true, tools: [...tools] }]
      })
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
          if (ev === 'meta') { cur = d.conv_id; setConvId(cur); if (!convId) setMsgs(m => m) }
          else if (ev === 'reasoning') { asst.reasoning += d.text || ''; push() }
          else if (ev === 'delta') { asst.content += d.text || ''; push() }
          else if (ev === 'tool') { tools.push({ ...d, id: tools.length }); push() }
          else if (ev === 'error') { asst.content += '\n✗ ' + (d.error || ''); push() }
          else if (ev === 'done') { setStatus(`完成 · ${d.secs?.toFixed(1)}s`) }
        }
      }
      await refresh(); if (cur) await openConv(cur)
    } catch (e) {
      setStatus('✗ ' + e.message)
    } finally { setBusy(false) }
  }

  const cur = (showArchived ? archived : convs)
  return (
    <div className="app">
      <aside className="side">
        <h1>AGENTD <span style={{ float: 'right', cursor: 'pointer' }} onClick={() => { setShowArchived(!showArchived) }}>{showArchived ? '活跃' : '归档'}</span></h1>
        <div className="convs">
          <div className="conv" onClick={() => openConv(null)}>＋ 新会话</div>
          {cur.map(c => (
            <div key={c.ID} className={'conv' + (c.ID === convId ? ' on' : '')} onClick={() => openConv(c.ID)}>
              {c.Title || '(无标题)'} <small>{c.N}</small>
            </div>
          ))}
        </div>
      </aside>
      <main className="main">
        <div className="bar">
          <select value={profile} onChange={e => setProfile(e.target.value)}>
            {profiles.map(p => <option key={p.name} value={p.name}>{p.name} · {p.model} · {p.reasoning_effort || '-'}</option>)}
          </select>
          <span className="sp" />
          <span style={{ color: '#8b93a3' }}>{convId || '新会话'}</span>
          {convId && <button onClick={async () => { await fetch('/api/conv/' + convId, { method: 'DELETE' }); setConvId(null); setMsgs([]); refresh() }}>归档</button>}
        </div>
        <div className="msgs" ref={boxRef}>
          {msgs.map((m, i) => (
            <div key={i} className={'msg ' + m.role}>
              <div className="who">{m.role}{m.seq ? ' #' + m.seq : ''}</div>
              {m.reasoning ? <div className="think">{m.reasoning}</div> : null}
              {(m.tools || []).map(t => <ToolCard key={t.id} t={t} />)}
              <div className="body">{m.content}</div>
              {(m.images || []).map((u, k) => <img key={k} src={u} />)}
            </div>
          ))}
          {busy ? <div className="who">…</div> : null}
        </div>
        <div className="status">{status}</div>
        <div className="input">
          <textarea value={text} onChange={e => setText(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}
            placeholder="Ctrl+Enter 发送" />
          <button onClick={send} disabled={busy}>发送</button>
        </div>
      </main>
    </div>
  )
}
