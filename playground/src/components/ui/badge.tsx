import { cn } from '@/lib/utils'

interface BadgeProps {
  className?: string
  children: React.ReactNode
  tone?: 'default' | 'running' | 'stopped'
}

const toneClass: Record<NonNullable<BadgeProps['tone']>, string> = {
  default: 'badge',
  running: 'badge badge-running',
  stopped: 'badge badge-stopped',
}

export function Badge({ className, children, tone = 'default' }: BadgeProps) {
  return <span className={cn(toneClass[tone], className)}>{children}</span>
}
