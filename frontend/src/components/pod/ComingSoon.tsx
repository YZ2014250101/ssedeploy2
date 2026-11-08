// 该组件为通用"开发中"占位，用于未实现的 Tab
export function ComingSoon({ title }: { title: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-16 text-center shadow-sm">
      <div className="text-sm font-medium text-slate-700">{title}</div>
      <div className="mt-1 text-xs text-slate-400">该模块开发中，敬请期待。</div>
    </div>
  )
}
