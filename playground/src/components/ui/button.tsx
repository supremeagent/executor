import * as React from 'react'
import { cn } from '@/lib/utils'

type ButtonVariant = 'default' | 'secondary' | 'outline' | 'destructive'
type ButtonSize = 'default' | 'sm'

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

const variantClasses: Record<ButtonVariant, string> = {
  default: 'btn btn-primary',
  secondary: 'btn btn-secondary',
  outline: 'btn btn-outline',
  destructive: 'btn btn-danger',
}

const sizeClasses: Record<ButtonSize, string> = {
  default: '',
  sm: 'btn-sm',
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'default', size = 'default', ...props }, ref) => (
    <button
      className={cn(variantClasses[variant], sizeClasses[size], className)}
      ref={ref}
      {...props}
    />
  ),
)

Button.displayName = 'Button'
