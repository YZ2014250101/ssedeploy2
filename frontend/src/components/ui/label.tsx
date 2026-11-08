import * as React from "react"
import { cn } from "@/lib/utils"

// 原生 label，不额外引入 Radix 依赖
const Label = React.forwardRef<HTMLLabelElement, React.LabelHTMLAttributes<HTMLLabelElement>>(
  ({ className, ...props }, ref) => (
    <label
      ref={ref}
      className={cn("text-sm font-medium text-slate-700 leading-none", className)}
      {...props}
    />
  ),
)
Label.displayName = "Label"

export { Label }
