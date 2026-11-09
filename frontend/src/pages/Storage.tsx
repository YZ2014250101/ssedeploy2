import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { AlertTriangle, Database, HardDrive, Layers, RefreshCw, ShieldAlert } from "lucide-react"
import { toast } from "sonner"
import { storageApi, type StorageOverviewData } from "@/api/storage"
import { getUser } from "@/lib/auth"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

// Kubernetes Quantity 单位换算表（二进制与十进制后缀）
const BINARY_UNITS: Record<string, number> = {
  Ki: 1024,
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
  Pi: 1024 ** 5,
  Ei: 1024 ** 6,
}
const DECIMAL_UNITS: Record<string, number> = {
  k: 1e3,
  K: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
}

// 把 "5Gi" / "500Mi" / "1073741824" 这类容量字符串换算为 GiB 数值
function quantityToGiB(raw: string): number {
  if (!raw || raw === "-") return 0
  const match = /^([0-9.]+)\s*([A-Za-z]*)$/.exec(raw.trim())
  if (!match) return 0
  const value = Number(match[1])
  if (!Number.isFinite(value)) return 0
  const unit = match[2]
  // 无单位时 Quantity 以字节为单位
  if (unit === "") return value / 1024 ** 3
  if (unit in BINARY_UNITS) return (value * BINARY_UNITS[unit]) / 1024 ** 3
  if (unit in DECIMAL_UNITS) return (value * DECIMAL_UNITS[unit]) / 1024 ** 3
  return 0
}

// 总容量展示：小于 1024Gi 用 Gi，否则换算为 Ti
function formatTotalCapacity(gib: number): string {
  if (gib <= 0) return "0 Gi"
  if (gib >= 1024) return `${(gib / 1024).toFixed(2)} Ti`
  return `${gib.toFixed(gib < 10 ? 2 : 1)} Gi`
}

// PV 状态配色：Bound 绿、Available 蓝、异常红
function pvStatusVariant(status: string): "success" | "default" | "destructive" | "secondary" {
  if (status === "Bound") return "success"
  if (status === "Available") return "default"
  if (status === "Failed" || status === "Lost") return "destructive"
  return "secondary"
}

// 回收策略配色：Delete 红色警告、Retain 绿色
function reclaimVariant(policy: string): "destructive" | "success" | "warning" | "secondary" {
  if (policy === "Delete") return "destructive"
  if (policy === "Retain") return "success"
  if (policy === "Recycle") return "warning"
  return "secondary"
}

// 顶部统计卡片
function StatCard({
  icon: Icon,
  label,
  value,
  hint,
}: {
  icon: typeof HardDrive
  label: string
  value: string | number
  hint?: string
}) {
  return (
    <Card className="flex items-center gap-4 p-5">
      <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-blue-50 text-blue-600">
        <Icon className="h-5 w-5" />
      </div>
      <div className="min-w-0">
        <div className="text-sm text-slate-500">{label}</div>
        <div className="mt-0.5 text-2xl font-semibold tracking-tight text-slate-900">{value}</div>
        {hint && <div className="mt-0.5 text-xs text-slate-400">{hint}</div>}
      </div>
    </Card>
  )
}

// 空数据占位行
function EmptyRow({ colSpan }: { colSpan: number }) {
  return (
    <TableRow>
      <TableCell colSpan={colSpan} className="py-12 text-center text-sm text-slate-400">
        暂无数据
      </TableCell>
    </TableRow>
  )
}

// 加载骨架
function TableSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-2.5">
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} className="h-10 w-full" />
      ))}
    </div>
  )
}

// 403 无权限页：非管理员直接渲染，不发请求
function NoPermission({ onBack }: { onBack: () => void }) {
  return (
    <div className="flex min-h-[60vh] items-center justify-center">
      <Card className="w-full max-w-md p-10 text-center">
        <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-red-50 text-red-500">
          <ShieldAlert className="h-7 w-7" />
        </div>
        <h1 className="mt-5 text-xl font-semibold text-slate-900">403 无权限访问</h1>
        <p className="mt-2 text-sm text-slate-500">该页面为管理员专属存储大盘，当前账号没有访问权限。</p>
        <Button className="mt-6" onClick={onBack}>
          返回首页
        </Button>
      </Card>
    </div>
  )
}

export default function Storage() {
  const navigate = useNavigate()
  // 角色读取自本地登录态；非管理员不发起任何网络请求
  const isAdmin = getUser()?.role === "admin"

  const [data, setData] = useState<StorageOverviewData | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState("")

  // 拉取存储大盘数据
  const load = useCallback(async () => {
    if (!isAdmin) return
    setLoading(true)
    try {
      const result = await storageApi.overview()
      setData(result)
      setError("")
    } catch (err) {
      const message = (err as Error).message
      setError(message)
      toast.error(message)
    } finally {
      setLoading(false)
    }
  }, [isAdmin])

  useEffect(() => {
    void load()
  }, [load])

  // 总分配容量 = 所有 PV 容量之和
  const totalCapacity = useMemo(
    () => (data?.pvs ?? []).reduce((sum, pv) => sum + quantityToGiB(pv.capacity), 0),
    [data],
  )

  // 主动刷新
  async function handleRefresh() {
    setRefreshing(true)
    try {
      const result = await storageApi.overview()
      setData(result)
      setError("")
      toast.success("已刷新最新数据")
    } catch (err) {
      const message = (err as Error).message
      setError(message)
      toast.error(message)
    } finally {
      setRefreshing(false)
    }
  }

  // 非管理员：直接展示 403，不发请求
  if (!isAdmin) {
    return <NoPermission onBack={() => navigate("/")} />
  }

  const firstLoading = loading && !data

  return (
    <div className="mx-auto max-w-7xl space-y-5">
      {/* 标题与操作区 */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900">存储大盘</h1>
          <p className="mt-1 text-sm text-slate-500">管理员专属 · 集群 PV / PVC / StorageClass 资源总览</p>
        </div>
        <Button variant="outline" onClick={handleRefresh} disabled={refreshing || loading}>
          <RefreshCw className={refreshing ? "h-4 w-4 animate-spin" : "h-4 w-4"} />
          刷新
        </Button>
      </div>

      {/* 顶部看板 */}
      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard
          icon={HardDrive}
          label="PV 总数"
          value={firstLoading ? "—" : data?.pvs.length ?? 0}
          hint="物理卷 PersistentVolume"
        />
        <StatCard
          icon={Database}
          label="总分配容量"
          value={firstLoading ? "—" : formatTotalCapacity(totalCapacity)}
          hint="全部 PV 容量之和"
        />
        <StatCard
          icon={Layers}
          label="SC 总数"
          value={firstLoading ? "—" : data?.storageClasses.length ?? 0}
          hint="存储类 StorageClass"
        />
      </div>

      {/* 请求失败：首次加载失败展示整块错误，已有数据时仅顶部提示 */}
      {error && !data && (
        <Card className="flex flex-col items-center justify-center gap-3 p-10 text-center">
          <AlertTriangle className="h-8 w-8 text-amber-500" />
          <div className="text-sm text-slate-600">{error}</div>
          <Button variant="outline" onClick={() => void load()}>
            重试
          </Button>
        </Card>
      )}

      {error && data && (
        <div className="flex items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-4 py-2.5 text-sm text-amber-700">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span className="flex-1">{error}</span>
          <button type="button" className="text-xs underline" onClick={() => void load()}>
            重试
          </button>
        </div>
      )}

      {/* 主体：三张表格 */}
      {!error || data ? (
        <Card className="p-5">
          {firstLoading ? (
            <div className="space-y-4">
              <Skeleton className="h-9 w-72" />
              <TableSkeleton />
            </div>
          ) : (
            <Tabs defaultValue="pv">
              <TabsList>
                <TabsTrigger value="pv">物理卷 PV ({data?.pvs.length ?? 0})</TabsTrigger>
                <TabsTrigger value="pvc">存储声明 PVC ({data?.pvcs.length ?? 0})</TabsTrigger>
                <TabsTrigger value="sc">存储类 SC ({data?.storageClasses.length ?? 0})</TabsTrigger>
              </TabsList>

              {/* Tab 1：物理卷 */}
              <TabsContent value="pv">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>PV 名称</TableHead>
                      <TableHead>容量</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead>回收策略</TableHead>
                      <TableHead>绑定的 PVC</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(data?.pvs ?? []).map((pv) => (
                      <TableRow key={pv.name}>
                        <TableCell className="font-mono text-xs text-slate-800">{pv.name}</TableCell>
                        <TableCell>{pv.capacity}</TableCell>
                        <TableCell>
                          <Badge variant={pvStatusVariant(pv.status)}>{pv.status || "-"}</Badge>
                        </TableCell>
                        <TableCell>
                          <Badge variant={reclaimVariant(pv.reclaimPolicy)}>{pv.reclaimPolicy || "-"}</Badge>
                        </TableCell>
                        <TableCell className="font-mono text-xs text-slate-600">
                          {pv.claimName || "-"}
                        </TableCell>
                      </TableRow>
                    ))}
                    {(data?.pvs ?? []).length === 0 && <EmptyRow colSpan={5} />}
                  </TableBody>
                </Table>
              </TabsContent>

              {/* Tab 2：存储声明 */}
              <TabsContent value="pvc">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>名称</TableHead>
                      <TableHead>命名空间</TableHead>
                      <TableHead>容量</TableHead>
                      <TableHead>状态</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(data?.pvcs ?? []).map((pvc) => (
                      <TableRow key={`${pvc.namespace}/${pvc.name}`}>
                        <TableCell className="font-mono text-xs text-slate-800">{pvc.name}</TableCell>
                        <TableCell>{pvc.namespace}</TableCell>
                        <TableCell>{pvc.capacity}</TableCell>
                        <TableCell>
                          <Badge variant={pvStatusVariant(pvc.status)}>{pvc.status || "-"}</Badge>
                        </TableCell>
                      </TableRow>
                    ))}
                    {(data?.pvcs ?? []).length === 0 && <EmptyRow colSpan={4} />}
                  </TableBody>
                </Table>
              </TabsContent>

              {/* Tab 3：存储类 */}
              <TabsContent value="sc">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>名称</TableHead>
                      <TableHead>Provisioner</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(data?.storageClasses ?? []).map((sc) => (
                      <TableRow key={sc.name}>
                        <TableCell className="font-mono text-xs text-slate-800">{sc.name}</TableCell>
                        <TableCell>{sc.provisioner || "-"}</TableCell>
                      </TableRow>
                    ))}
                    {(data?.storageClasses ?? []).length === 0 && <EmptyRow colSpan={2} />}
                  </TableBody>
                </Table>
              </TabsContent>
            </Tabs>
          )}
        </Card>
      ) : null}
    </div>
  )
}
