export type ExecutorType = 'codex' | 'claude_code'

export interface ExecuteRequest {
  prompt: string
  executor: ExecutorType
  working_dir: string
}

export interface ExecuteResponse {
  session_id: string
  status: string
}

export interface ContinueRequest {
  message: string
}

export interface ControlResponseRequest {
  request_id: string
  decision: 'approve' | 'deny'
  reason?: string
}

export interface UnifiedEventContent {
  source: string
  source_type: string
  category: string
  phase?: string
  text?: string
  tool_name?: string
  request_id?: string
  status?: string
  raw?: unknown
}

export interface EventItem {
  session_id?: string
  executor?: string
  seq?: number
  timestamp?: string
  type: string
  content: unknown | UnifiedEventContent
}

export interface ApiSessionItem {
  session_id: string
  title: string
  status: 'running' | 'done' | 'interrupted'
  created_at: string
  updated_at: string
  executor: ExecutorType
}

export interface SessionItem {
  id: string
  title: string
  status: 'running' | 'done' | 'interrupted'
  createdAt: string
  updatedAt: string
  executor: ExecutorType
}
