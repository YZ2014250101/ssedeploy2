import { useState } from "react"
import { Loader2, Plus, Save, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { podApi, type EnvVar, type PodDetail } from "@/api/pod"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface SettingsTabProps {
  detail: PodDetail
  onUpdated: () => void
  onDeleted: () => void
}

// 设置 Tab：修改 Pod 配置 + 危险操作
export function SettingsTab({ detail, onUpdated, onDeleted }: SettingsTabProps) {
  const [image, setImage] = useState(detail.image)
  const [cpu, setCpu] = useState(detail.cpu)
  const [memory, setMemory] = useState(detail.memory)
  const [envs, setEnvs] = useState<EnvVar[]>(detail.env.length > 0 ? detail.env : [{ key: "", value: "" }])
  const [command, setCommand] = useState(detail.command.join("\n"))
  const [saving, setSaving] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [confirmName, setConfirmName] = useState("")
  const [deleting, setDeleting] = useState(false)

  function updateEnv(index: number, key: keyof EnvVar, value: string) {
    setEnvs((prev) => prev.map((item, i) => (i === index ? { ...item, [key]: value } : item)))
  }

  async function handleSave() {
    if (!image.trim()) {
      toast.error("容器镜像不能为空")
      return
    }
    setSaving(true)
    try {
      await podApi.update(detail.name, {
        image: image.trim(),
        cpu: cpu.trim(),
        memory: memory.trim(),
        env: envs.filter((item) => item.key.trim() !== ""),
        command: command
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean),
      })
      toast.success("配置已保存，Pod 将自动滚动更新")
      onUpdated()
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete() {
    if (confirmName !== detail.name) {
      toast.error("请输入完整的 Pod 名称以确认删除")
      return
    }
    setDeleting(true)
    try {
      await podApi.remove(detail.name)
      toast.success("Pod 已删除")
      setDeleteOpen(false)
      onDeleted()
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>基础配置</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="setting-image">容器镜像</Label>
            <Input id="setting-image" value={image} onChange={(e) => setImage(e.target.value)} />
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            <div className="space-y-1.5">
              <Label htmlFor="setting-cpu">CPU（核）</Label>
              <Input id="setting-cpu" value={cpu} onChange={(e) => setCpu(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="setting-memory">内存</Label>
              <Input id="setting-memory" value={memory} onChange={(e) => setMemory(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="setting-disk">存储</Label>
              <Input id="setting-disk" value={detail.disk} disabled />
              <p className="text-xs text-slate-400">存储容量暂不支持在线扩容</p>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>环境变量</Label>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setEnvs((prev) => [...prev, { key: "", value: "" }])}
              >
                <Plus className="h-3.5 w-3.5" />
                添加
              </Button>
            </div>
            {envs.map((item, index) => (
              <div key={index} className="flex items-center gap-2">
                <Input
                  value={item.key}
                  onChange={(e) => updateEnv(index, "key", e.target.value)}
                  placeholder="变量名"
                />
                <Input
                  value={item.value}
                  onChange={(e) => updateEnv(index, "value", e.target.value)}
                  placeholder="变量值"
                />
                <Button
                  variant="ghost"
                  size="icon"
                  className="text-slate-400"
                  disabled={envs.length === 1}
                  onClick={() => setEnvs((prev) => prev.filter((_, i) => i !== index))}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="setting-command">启动命令（每行一个参数）</Label>
            <textarea
              id="setting-command"
              rows={3}
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder={"留空则默认使用：sleep infinity"}
              className="w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
            />
          </div>

          <div className="flex justify-end">
            <Button onClick={handleSave} disabled={saving}>
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              保存配置
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* 危险操作区 */}
      <Card className="border-red-200">
        <CardHeader>
          <CardTitle className="text-red-600">危险操作</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-slate-500">删除 Pod 会同时删除其 Deployment 与数据盘，且不可恢复。</p>
          <Button variant="destructive" onClick={() => setDeleteOpen(true)}>
            <Trash2 className="h-4 w-4" />
            删除 Pod
          </Button>
        </CardContent>
      </Card>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>确认删除 {detail.name}</DialogTitle>
            <DialogDescription>
              请输入 Pod 名称 <span className="font-mono font-medium text-slate-900">{detail.name}</span> 以确认删除。
            </DialogDescription>
          </DialogHeader>
          <Input value={confirmName} onChange={(e) => setConfirmName(e.target.value)} placeholder={detail.name} />
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              取消
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleting}>
              {deleting && <Loader2 className="h-4 w-4 animate-spin" />}
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
