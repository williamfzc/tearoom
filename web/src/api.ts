// The hosted management API. The admin token is stored locally (it is the
// Phase 1 dev-admin bearer; Phase 2 replaces it with a GitHub session).
// VITE_API_BASE points at the Render service in production; dev proxies.

const base = import.meta.env.VITE_API_BASE ?? ''

const TOKEN_KEY = 'tearoom-admin-token'

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

async function req(method: string, path: string, body?: unknown): Promise<any> {
  const res = await fetch(base + path, {
    method,
    headers: {
      Authorization: `Bearer ${getToken()}`,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const j = await res.json()
      if (typeof j.message === 'string') message = j.message
    } catch {
      // keep the status line
    }
    throw new Error(message)
  }
  if (res.status === 204) return null
  return res.json()
}

export interface Room {
  id: string
  name: string
  created_at?: string
}

export interface Credential {
  id: number
  actor: string
  capabilities: string
  expires_at?: string | null
  revoked_at?: string | null
  created_at?: string
}

export interface RoomEvent {
  id: string
  seq: number
  actor: string
  at?: string
  type?: string
  body?: string
  data?: unknown
  caused_by?: string
}

export const api = {
  login: () => req('POST', '/api/hosted/login'),
  rooms: () => req('GET', '/api/hosted/rooms'),
  createRoom: (name: string) => req('POST', '/api/hosted/rooms', { name }),
  credentials: (room: string): Promise<{ credentials: Credential[] }> =>
    req('GET', `/api/hosted/rooms/${room}/credentials`),
  issue: (room: string, actor: string, capabilities: string, ttlSeconds?: number) =>
    req('POST', `/api/hosted/rooms/${room}/credentials`, {
      actor,
      capabilities,
      ttl_seconds: ttlSeconds,
    }),
  revoke: (room: string, id: number) =>
    req('DELETE', `/api/hosted/rooms/${room}/credentials/${id}`),
}

// fetchEvents reads a room's event history through the room event API using
// a room credential (not the admin token): registration first (identity, not
// content — any capability may register), then the feed from cursor 0.
export async function fetchEvents(room: string, actor: string, credential: string): Promise<RoomEvent[]> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${credential}`,
    'Content-Type': 'application/json',
  }
  await fetch(`${base}/r/${room}/api/actors`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ id: actor, role: '' }),
  })
  const res = await fetch(`${base}/r/${room}/api/events?actor=${encodeURIComponent(actor)}&after=0`, {
    headers: { Authorization: `Bearer ${credential}` },
  })
  if (!res.ok) {
    throw new Error(`${res.status} ${res.statusText}`)
  }
  const feed = await res.json()
  return feed.events ?? []
}
