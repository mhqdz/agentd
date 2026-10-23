import React, { useEffect, useRef, useState } from 'react'

const api = (p, opt) => fetch(p, opt).then(r => r.json())

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
      let buf = '', asst = { role: 'assistant', content: '', reasoning: '', tools: [] }
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
          if (ev === 'meta') { cur = d.conv_id; setConvId(cur) }
          else if (ev === 'reasoning') { asst.reasoning += d.text || ''; push() }
          else if (ev === 'delta') { asst.content += d.text || ''; push() }
          else if (ev === 'tool') { asst.tools.push({ name: d.name, args: d.args, text: d.text, image: d.image }); push() }
          else if (ev === 'error') { asst.content += '\n✗ ' + (d.error || ''); push() }
          else if (ev === 'done') { setStatus(`完成 · ${(d.secs || 0).toFixed(1)}s`) }
        }
      }
      await refresh(); if (cur) await openConv(cur)
    } catch (e) { setStatus('✗ ' + e.message) } finally { setBusy(false) }
  }

  const list = showArchived ? archived : convs
  return (
    <div className="app">
      <header className="ef-cmdbar top">
        <strong className="ef-pair"><span className="ef-eyebrow">Agentd</span><span>终端</span></strong>
        <span className="ef-chip">{profile}</span>
        <select className="sel" value={profile} onChange={e => setProfile(e.target.value)}>
          {profiles.map(p => <option key={p.name} value={p.name}>{p.name} · {p.model}</option>)}
        </select>
        <span style={{ flex: 1 }} />
        <span className="ef-eyebrow">{convId ? '#' + convId.slice(0, 8) : 'new'}</span>
        <button className="ef-btn" onClick={() => { setShowArchived(!showArchived) }}>{showArchived ? '记录' : '归档'}</button>
        <button className="ef-btn" onClick={() => openConv(null)}>新建</button>
      </header>
      <div className="body">
        <aside className="side">
          <div className="ef-records">
            {list.length === 0 ? <div className="ef-empty">没有记录</div> : null}
            {list.map((c, i) => (
              <div key={c.ID} className={'ef-record rec-row' + (c.ID === convId ? ' on' : '')} onClick={() => openConv(c.ID)}>
                <span><span className="ef-eyebrow">No.{String(i + 1).padStart(3, '0')}</span> {c.Title || '(无标题)'}</span>
                <small>{c.N}</small>
              </div>
            ))}
          </div>
        </aside>
        <main className="main">
          <div className="thread">
            {msgs.map((m, i) => (
              <div key={i} className={'msg msg-' + m.role}>
                <div className="ef-pair"><span className="ef-eyebrow">{m.role}</span><span>{m.seq ? '#' + m.seq : ''}</span></div>
                {m.reasoning ? <div className="think">{m.reasoning}</div> : null}
                {(m.tools || []).map((t, k) => (
                  <details className="ef-panel toolbox" key={k}>
                    <summary><span className="ef-eyebrow">{t.name}</span> {(t.args || '').slice(0, 80)}</summary>
                    {t.text ? <pre>{t.text}</pre> : null}
                  </details>
                ))}
                <div className="body-t">{m.content}</div>
                {(m.images || []).map((u, k) => <img key={k} src={u} style={{ maxWidth: '100%', marginTop: '.5rem' }} />)}
              </div>
            ))}
            <div ref={endRef} />
          </div>
          <div className="status">{status}</div>
          <div className="composer">
            <textarea className="ef-field" value={text} onChange={e => setText(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) send() }}
              placeholder="Ctrl+Enter 发送" />
            <button className="ef-btn" onClick={send} disabled={busy}>发送</button>
          </div>
        </main>
      </div>
    </div>
  )
}
