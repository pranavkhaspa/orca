import type { Findings, Health, Meta, StreamEvent } from './types'

// The API base is empty in dev (Vite proxies /api to the Go service) and set to
// the Render URL in production via VITE_API_BASE.
const BASE = (import.meta.env.VITE_API_BASE ?? '').replace(/\/$/, '')

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { signal })
  if (!res.ok) throw new Error(`${path} → HTTP ${res.status}`)
  return (await res.json()) as T
}

export function fetchMeta(signal?: AbortSignal) {
  return getJSON<Meta>('/api/meta', signal)
}

export function fetchHealth(signal?: AbortSignal) {
  return getJSON<Health>('/healthz', signal)
}

export async function ask(query: string, signal?: AbortSignal): Promise<Findings> {
  const res = await fetch(`${BASE}/api/ask`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ query }),
    signal,
  })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const j = (await res.json()) as { error?: string }
      if (j.error) msg = j.error
    } catch {
      /* body was not JSON; keep the status message */
    }
    throw new Error(msg)
  }
  return (await res.json()) as Findings
}

export interface StreamHandlers {
  onAgent(run: StreamEvent & { type: 'agent' }): void
  onAnswer(answer: string, lang: string): void
  onDone(final: Findings): void
  onError(message: string): void
}

// Streams the agent trace as it happens. EventSource cannot be used because the
// endpoint is a GET with a query string, but a long-lived fetch body gives us
// readable incremental chunks without a custom event-target shim.
export function streamAsk(
  query: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const url = `${BASE}/api/ask/stream?q=${encodeURIComponent(query)}`
  return (async () => {
    let res: Response
    try {
      res = await fetch(url, { signal, headers: { accept: 'text/event-stream' } })
    } catch (err) {
      if (signal?.aborted) return
      handlers.onError(err instanceof Error ? err.message : 'network error')
      return
    }
    if (!res.ok || !res.body) {
      handlers.onError(`stream → HTTP ${res.status}`)
      return
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''

    const handle = (block: string) => {
      let event = 'message'
      const data: string[] = []
      for (const line of block.split('\n')) {
        if (line.startsWith('event:')) event = line.slice(6).trim()
        else if (line.startsWith('data:')) data.push(line.slice(5).trim())
      }
      if (data.length === 0) return
      let payload: unknown
      try {
        payload = JSON.parse(data.join('\n'))
      } catch {
        return
      }
      const p = payload as { type?: string } & Record<string, unknown>
      // The event name and the payload's own type must agree; a mismatch means
      // the frame is malformed, so it is dropped rather than half-parsed.
      if (event !== p.type) return
      if (p.type === 'agent') handlers.onAgent(p as never)
      else if (p.type === 'answer') {
        handlers.onAnswer(String(p.answer ?? ''), String(p.answer_lang ?? 'en'))
      } else if (p.type === 'done' && p.final) {
        handlers.onDone(p.final as Findings)
      }
    }

    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        // SSE frames are separated by a blank line.
        let idx: number
        while ((idx = buf.search(/\r?\n\r?\n/)) >= 0) {
          const block = buf.slice(0, idx)
          buf = buf.slice(idx + (buf[idx] === '\r' ? 4 : 2))
          handle(block)
        }
      }
    } catch (err) {
      if (!signal?.aborted) {
        handlers.onError(err instanceof Error ? err.message : 'stream interrupted')
      }
    }
  })()
}
