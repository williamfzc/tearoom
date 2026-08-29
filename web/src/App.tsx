import { useCallback, useEffect, useState } from 'react'
import { api, clearToken, fetchEvents, getToken, setToken, type Credential, type Room, type RoomEvent } from './api'

type View = { kind: 'login' } | { kind: 'rooms' } | { kind: 'room'; id: string; name: string }

export default function App() {
  const [view, setView] = useState<View>(() =>
    getToken() ? { kind: 'rooms' } : { kind: 'login' },
  )
  return (
    <div className="shell">
      <header className="topbar">
        <span className="wordmark">tearoom</span>
        {getToken() && (
          <button
            className="ghost"
            onClick={() => {
              clearToken()
              setView({ kind: 'login' })
            }}
          >
            sign out
          </button>
        )}
      </header>
      {view.kind === 'login' && <Login onEnter={() => setView({ kind: 'rooms' })} />}
      {view.kind === 'rooms' && (
        <Rooms onOpen={(id, name) => setView({ kind: 'room', id, name })} />
      )}
      {view.kind === 'room' && (
        <Room id={view.id} name={view.name} onBack={() => setView({ kind: 'rooms' })} />
      )}
    </div>
  )
}

function Login({ onEnter }: { onEnter: () => void }) {
  const [token, setToken_] = useState(getToken())
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const enter = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      setToken(token.trim())
      await api.login() // validates the token against the server
      onEnter()
    } catch (e) {
      clearToken()
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }, [token, onEnter])

  return (
    <main className="centered">
      <section className="card login-card">
        <h1>Hosted rooms</h1>
        <p className="muted">
          Enter the admin token issued for this deployment. It is stored only in this
          browser.
        </p>
        <input
          type="password"
          placeholder="admin token"
          value={token}
          autoFocus
          onChange={(e) => setToken_(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && enter()}
        />
        {error && <p className="error">{error}</p>}
        <button className="primary" disabled={busy || !token.trim()} onClick={enter}>
          {busy ? 'verifying…' : 'enter'}
        </button>
      </section>
    </main>
  )
}

function Rooms({ onOpen }: { onOpen: (id: string, name: string) => void }) {
  const [rooms, setRooms] = useState<Room[] | null>(null)
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    setError('')
    try {
      const res = await api.rooms()
      setRooms(res.rooms ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const create = async () => {
    setBusy(true)
    setError('')
    try {
      await api.createRoom(name.trim())
      setName('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main>
      <div className="row">
        <h1>Rooms</h1>
        <div className="spacer" />
        <input placeholder="room name" value={name} onChange={(e) => setName(e.target.value)} />
        <button className="primary" disabled={busy || !name.trim()} onClick={create}>
          create
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {rooms === null ? (
        <p className="muted">loading…</p>
      ) : rooms.length === 0 ? (
        <p className="muted">No rooms yet — create one to issue credentials.</p>
      ) : (
        <ul className="list">
          {rooms.map((room) => (
            <li key={room.id} className="card clickable" onClick={() => onOpen(room.id, room.name)}>
              <span className="strong">{room.name}</span>
              <code>{room.id}</code>
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}

function Room({ id, name, onBack }: { id: string; name: string; onBack: () => void }) {
  return (
    <main>
      <div className="row">
        <button className="ghost" onClick={onBack}>
          ← rooms
        </button>
        <h1>{name}</h1>
        <code className="room-id">{id}</code>
      </div>
      <Credentials room={id} />
      <History room={id} />
    </main>
  )
}

function Credentials({ room }: { room: string }) {
  const [creds, setCreds] = useState<Credential[] | null>(null)
  const [error, setError] = useState('')
  const [actor, setActor] = useState('')
  const [caps, setCaps] = useState(['read', 'append'])
  const [ttl, setTtl] = useState('')
  const [issued, setIssued] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const CAPS = ['read', 'append', 'manage']

  const load = useCallback(async () => {
    setError('')
    try {
      const res = await api.credentials(room)
      setCreds(res.credentials ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [room])

  useEffect(() => {
    void load()
  }, [load])

  const issue = async () => {
    setBusy(true)
    setError('')
    setIssued(null)
    try {
      const ttlSeconds = ttl ? Math.round(Number(ttl) * 60) : undefined
      const res = await api.issue(room, actor.trim(), caps.join(','), ttlSeconds)
      setIssued(res.token)
      setActor('')
      setTtl('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const revoke = async (cred: Credential) => {
    if (!window.confirm(`Revoke ${cred.actor}'s credential? It stops working immediately.`)) return
    setError('')
    try {
      await api.revoke(room, cred.id)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section>
      <h2>Credentials</h2>
      {error && <p className="error">{error}</p>}
      {issued && (
        <div className="card token-card">
          <p>
            Plaintext token — <strong>shown once</strong>. Copy it now.
          </p>
          <code className="token" onClick={() => navigator.clipboard?.writeText(issued)} title="click to copy">
            {issued}
          </code>
        </div>
      )}
      <div className="card form-row">
        <input placeholder="actor" value={actor} onChange={(e) => setActor(e.target.value)} />
        {CAPS.map((c) => (
          <label key={c} className="cap">
            <input
              type="checkbox"
              checked={caps.includes(c)}
              onChange={(e) =>
                setCaps(e.target.checked ? [...caps, c] : caps.filter((x) => x !== c))
              }
            />
            {c}
          </label>
        ))}
        <input
          placeholder="ttl minutes (blank = none)"
          type="number"
          min="1"
          value={ttl}
          onChange={(e) => setTtl(e.target.value)}
        />
        <button className="primary" disabled={busy || !actor.trim() || caps.length === 0} onClick={issue}>
          issue
        </button>
      </div>
      {creds === null ? (
        <p className="muted">loading…</p>
      ) : creds.length === 0 ? (
        <p className="muted">No credentials yet.</p>
      ) : (
        <table className="creds">
          <thead>
            <tr>
              <th>actor</th>
              <th>capabilities</th>
              <th>expires</th>
              <th>status</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {creds.map((cred) => (
              <tr key={cred.id}>
                <td className="strong">{cred.actor}</td>
                <td>
                  <code>{cred.capabilities}</code>
                </td>
                <td className="muted">{cred.expires_at ?? 'never'}</td>
                <td className={cred.revoked_at ? 'danger' : 'ok'}>
                  {cred.revoked_at ? 'revoked' : 'live'}
                </td>
                <td className="right">
                  {!cred.revoked_at && (
                    <button className="ghost danger-hover" onClick={() => revoke(cred)}>
                      revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function History({ room }: { room: string }) {
  const [cred, setCred] = useState('')
  const [actor, setActor] = useState('')
  const [events, setEvents] = useState<RoomEvent[] | null>(null)
  const [error, setError] = useState('')

  const read = async () => {
    setError('')
    setEvents(null)
    try {
      setEvents(await fetchEvents(room, actor.trim(), cred.trim()))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section>
      <h2>Room history</h2>
      <p className="muted">
        Read the room's events with one of its credentials — paste it and the actor it was
        issued for.
      </p>
      <div className="card form-row">
        <input placeholder="room credential (tr_…)" value={cred} onChange={(e) => setCred(e.target.value)} />
        <input placeholder="actor" value={actor} onChange={(e) => setActor(e.target.value)} />
        <button className="ghost" disabled={!cred.trim() || !actor.trim()} onClick={read}>
          read
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {events !== null &&
        (events.length === 0 ? (
          <p className="muted">No events in this room.</p>
        ) : (
          <ul className="list events">
            {events.map((ev) => (
              <li key={ev.id} className="card">
                <div className="event-head">
                  <code>
                    #{ev.seq} {ev.id}
                  </code>
                  <span className="muted">
                    {ev.actor} · {ev.type ?? 'message'}
                    {ev.caused_by ? ` · caused by ${ev.caused_by}` : ''}
                  </span>
                </div>
                {ev.body && <p className="event-body">{ev.body}</p>}
              </li>
            ))}
          </ul>
        ))}
    </section>
  )
}
