import type { EventItem, UnifiedEventContent } from '@/types/chat'

export interface EventViewModel {
  headline: string
  details: string[]
  rawText: string
  unified: UnifiedEventContent | null
}

export function toUnifiedContent(content: unknown): UnifiedEventContent | null {
  if (!content || typeof content !== 'object') return null
  const obj = content as Record<string, unknown>
  if (typeof obj.category !== 'string') return null
  if (typeof obj.source !== 'string' || typeof obj.source_type !== 'string') return null
  return content as UnifiedEventContent
}

export function summarizeContent(content: unknown): string {
  if (typeof content === 'string') return content
  if (content == null) return ''
  try {
    return JSON.stringify(content, null, 2)
  } catch {
    return String(content)
  }
}

export function buildEventViewModel(event: EventItem): EventViewModel {
  const unified = toUnifiedContent(event.content)
  if (!unified) {
    return {
      headline: event.type,
      details: [],
      rawText: summarizeContent(event.content),
      unified: null,
    }
  }

  const headline = unified.summary || defaultHeadline(unified)
  const details: string[] = []

  if (unified.category) details.push(`Category: ${unified.category}`)
  if (unified.action) details.push(`Action: ${unified.action}`)
  if (unified.phase) details.push(`Phase: ${unified.phase}`)
  if (unified.tool_name) details.push(`Tool: ${unified.tool_name}`)
  if (unified.target) details.push(`Target: ${unified.target}`)
  if (unified.status) details.push(`Status: ${unified.status}`)

  const rawCandidate = unified.text || summarizeContent(unified.raw)

  return {
    headline,
    details,
    rawText: rawCandidate,
    unified,
  }
}

function defaultHeadline(content: UnifiedEventContent): string {
  switch (content.action) {
    case 'starting':
      return 'Initializing Execution Environment'
    case 'thinking':
      return 'Thinking Deeply'
    case 'reading':
      return content.target ? `Reading ${content.target}` : 'Reading File'
    case 'searching':
      return content.target ? `Searching: ${content.target}` : 'Searching'
    case 'tool_running':
      return content.tool_name ? `Calling Tool: ${content.tool_name}` : 'Calling Tool'
    case 'editing':
      return 'Modifying Code'
    case 'approval_required':
      return 'Waiting for Your Approval'
    case 'responding':
      return 'Generating Reply'
    case 'completed':
      return 'Execution Completed'
    case 'failed':
      return 'Execution Failed'
    default:
      return 'Processing'
  }
}
