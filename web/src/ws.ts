import { signal } from '@preact/signals'

// Server payloads follow docs/PROTOCOL.md; they are kept loosely typed on purpose.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type Data = any

export const isLocalPage = ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname)

export function store(key: string, value?: string | null): string | null {
  try {
    if (value === undefined) return localStorage.getItem(key)
    if (value === null) localStorage.removeItem(key)
    else localStorage.setItem(key, value)
  } catch {
    // Storage blocked (private browsing): the session still works, only reconnection is lost.
  }
  return null
}

/**
 * Opens the game websocket and keeps it open. `role` is only sent for the host machine pages;
 * `join` builds the join payload on every (re)connection, so a stored token is reused.
 */
export function connect(role: 'control' | 'host' | 'player', join: () => object = () => ({})) {
  const connected = signal(false)
  const error = signal('')
  const handlers = new Map<string, ((data: Data) => void)[]>()
  let ws: WebSocket
  let stopped = false

  const send = (type: string, data?: object) => {
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, data }))
  }
  const on = (type: string, handler: (data: Data) => void) => {
    handlers.set(type, [...(handlers.get(type) ?? []), handler])
  }
  const open = () => {
    const query = role === 'player' ? '' : `?role=${role}`
    ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws${query}`)
    ws.onopen = () => {
      connected.value = true
      send('join', join())
    }
    ws.onmessage = (e) => {
      const m = JSON.parse(e.data)
      if (m.type === 'error') error.value = m.data.message
      handlers.get(m.type)?.forEach((h) => h(m.data ?? {}))
    }
    ws.onclose = () => {
      connected.value = false
      if (!stopped) setTimeout(open, 1000)
    }
  }
  open()

  return {
    connected,
    error,
    send,
    on,
    stop: () => {
      stopped = true
      ws.close()
    },
  }
}
