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

  if (unified.category) details.push(`类别: ${unified.category}`)
  if (unified.action) details.push(`动作: ${unified.action}`)
  if (unified.phase) details.push(`阶段: ${unified.phase}`)
  if (unified.tool_name) details.push(`工具: ${unified.tool_name}`)
  if (unified.target) details.push(`目标: ${unified.target}`)
  if (unified.status) details.push(`状态: ${unified.status}`)

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
      return '正在初始化执行环境'
    case 'thinking':
      return '正在深度思考'
    case 'reading':
      return content.target ? `正在读取 ${content.target}` : '正在读取文件'
    case 'searching':
      return content.target ? `正在搜索：${content.target}` : '正在进行搜索'
    case 'tool_running':
      return content.tool_name ? `正在调用工具：${content.tool_name}` : '正在调用工具'
    case 'editing':
      return '正在修改代码'
    case 'approval_required':
      return '等待你的审批'
    case 'responding':
      return '正在生成回复'
    case 'completed':
      return '执行完成'
    case 'failed':
      return '执行失败'
    default:
      return '处理中'
  }
}
