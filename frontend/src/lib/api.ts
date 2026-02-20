import type {
  ContinueRequest,
  EventItem,
  ExecuteRequest,
  ExecuteResponse,
} from '@/types/chat'

const BASE = '/api'

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

export async function listEvents(sessionId: string, afterSeq = 0): Promise<EventItem[]> {
  const data = await request<{ session_id: string; events: EventItem[] }>(
    `${BASE}/execute/${sessionId}/events?after_seq=${afterSeq}&limit=300`,
  )
  return data.events ?? []
}

export function createStream(sessionId: string) {
  return new EventSource(`${BASE}/execute/${sessionId}/stream?return_all=false&debug=false`)
}
