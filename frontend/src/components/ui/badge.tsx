import * as React from "react"
import { cn } from "@/lib/utils"

// shadcn/ui Badge
const badgeBase =
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors"

type BadgeProps = React.HTMLAttributes<HTMLDivElement> & {
  variant?: "default" | "secondary" | "outline" | "destructive" | "success" | "warning"
}

const variants: Record<string, string> = {
  default: "border-transparent bg-blue-50 text-blue-700",
  secondary: "border-transparent bg-slate-100 text-slate-600",
  outline: "border-slate-200 text-slate-600",
  destructive: "border-transparent bg-red-50 text-red-700",
  success: "border-transparent bg-emerald-50 text-emerald-700",
  warning: "border-transparent bg-amber-50 text-amber-700",
}

function Badge({ className, variant = "default", ...props }: BadgeProps) {
  return <div className={cn(badgeBase, variants[variant], className)} {...props} />
}

export { Badge }
