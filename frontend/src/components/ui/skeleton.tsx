import { cn } from "@/lib/utils"

// shadcn/ui Skeleton
function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("animate-pulse rounded-md bg-slate-200/70", className)} {...props} />
}

export { Skeleton }
