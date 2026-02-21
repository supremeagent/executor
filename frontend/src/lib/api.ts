import type {
  ApiSessionItem,
  ControlResponseRequest,
  ContinueRequest,
  EventItem,
  ExecuteRequest,
  ExecuteResponse,
  SessionItem,
} from '@/types/chat'

const BASE = '/api'

export interface StreamConnection {
  close: () => void
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: {
      'Content-Type': 'application/json',
    },
    ...init,
  })

  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `Request failed: ${res.status}`)
  }

  return (await res.json()) as T
}

export function executeTask(payload: ExecuteRequest) {
  return request<ExecuteResponse>(`${BASE}/execute`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function continueTask(sessionId: string, payload: ContinueRequest) {
  return request<{ status: string }>(`${BASE}/execute/${sessionId}/continue`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function interruptTask(sessionId: string) {
  return request<{ status: string }>(`${BASE}/execute/${sessionId}/interrupt`, {
    method: 'POST',
  })
}

export function respondControl(sessionId: string, payload: ControlResponseRequest) {
  return request<{ status: string }>(`${BASE}/execute/${sessionId}/control`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function listEvents(sessionId: string, afterSeq = 0): Promise<EventItem[]> {
  const data = await request<{ session_id: string; events: EventItem[] }>(
    `${BASE}/execute/${sessionId}/events?after_seq=${afterSeq}&limit=300`,
  )
  return data.events ?? []
}

export async function listSessions(): Promise<SessionItem[]> {
  const data = await request<{ sessions: ApiSessionItem[] }>(`${BASE}/sessions`)
  const sessions = data.sessions ?? []

  return sessions.map((session) => ({
    id: session.session_id,
    title: session.title ?? '',
    status: session.status,
    createdAt: session.created_at,
    updatedAt: session.updated_at,
    executor: session.executor,
  }))
}

export function createStream(
  sessionId: string,
  onEvent: (evt: MessageEvent<string>) => void,
  onError?: (error: unknown) => void,
): StreamConnection {
  const controller = new AbortController()

  void (async () => {
    try {
      const res = await fetch(`${BASE}/execute/${sessionId}/stream?return_all=false&debug=false`, {
        headers: { Accept: 'text/event-stream' },
        signal: controller.signal,
      })

      if (!res.ok || !res.body) {
        throw new Error(`stream request failed: ${res.status}`)
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()

      let buffer = ''
      let eventType = 'message'
      let dataLines: string[] = []

      const flushEvent = () => {
        if (dataLines.length === 0) return
        const data = dataLines.join('\n')
        onEvent(new MessageEvent(eventType || 'message', { data }))
        eventType = 'message'
        dataLines = []
      }

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })

        let lineBreakIndex = buffer.indexOf('\n')
        while (lineBreakIndex >= 0) {
          let line = buffer.slice(0, lineBreakIndex)
          buffer = buffer.slice(lineBreakIndex + 1)
          lineBreakIndex = buffer.indexOf('\n')

          if (line.endsWith('\r')) line = line.slice(0, -1)

          if (line === '') {
            flushEvent()
            continue
          }
          if (line.startsWith(':')) continue
          if (line.startsWith('event:')) {
            eventType = line.slice(6).trim() || 'message'
            continue
          }
          if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).trimStart())
          }
        }
      }

      flushEvent()
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      onError?.(error)
    }
  })()

  return {
    close() {
      controller.abort()
    },
  }
}
