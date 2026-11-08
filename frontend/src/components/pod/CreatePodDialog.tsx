import { useEffect, useState } from "react"
import { Loader2, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { podApi, SPEC_PRESETS, type EnvVar } from "@/api/pod"
import { Button } from "@/components/ui/button"
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

interface CreatePodDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}

// Pod 名称规范与后端一致
const NAME_PATTERN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/

export function CreatePodDialog({ open, onOpenChange, onCreated }: CreatePodDialogProps) {
  const [name, setName] = useState("")
  const [image, setImage] = useState("ubuntu:22.04")
  const [cpu, setCpu] = useState("2")
  const [memory, setMemory] = useState("4Gi")
  const [disk, setDisk] = useState("5Gi")
  const [replicas, setReplicas] = useState("1")
  const [preset, setPreset] = useState(SPEC_PRESETS[1].label)
  const [envs, setEnvs] = useState<EnvVar[]>([{ key: "", value: "" }])
  const [command, setCommand] = useState("")
  const [submitting, setSubmitting] = useState(false)

  // 每次打开弹窗重置表单
  useEffect(() => {
    if (!open) return
    setName("")
    setImage("ubuntu:22.04")
    setCpu("2")
    setMemory("4Gi")
    setDisk("5Gi")
    setReplicas("1")
    setPreset(SPEC_PRESETS[1].label)
    setEnvs([{ key: "", value: "" }])
    setCommand("")
    setSubmitting(false)
  }, [open])

  // 选择预设时同步 CPU / 内存 / 存储
  function applyPreset(label: string) {
    setPreset(label)
    const item = SPEC_PRESETS.find((presetItem) => presetItem.label === label)
    if (item) {
      setCpu(item.cpu)
      setMemory(item.memory)
      setDisk(item.disk)
    }
  }

  function updateEnv(index: number, key: keyof EnvVar, value: string) {
    setEnvs((prev) => prev.map((item, i) => (i === index ? { ...item, [key]: value } : item)))
  }

  async function handleSubmit() {
    if (!name.trim()) {
      toast.error("请输入 Pod 名称")
      return
    }
    if (name.length > 40 || !NAME_PATTERN.test(name)) {
      toast.error("名称需为 1-40 位小写字母、数字或短横线，并以字母或数字开头结尾")
      return
    }
    if (!image.trim()) {
      toast.error("请输入容器镜像")
      return
    }
    const replicaCount = Number(replicas)
    if (!Number.isInteger(replicaCount) || replicaCount < 0) {
      toast.error("副本数需为非负整数")
      return
    }

    setSubmitting(true)
    try {
      await podApi.create({
        name: name.trim(),
        image: image.trim(),
        cpu: cpu.trim(),
        memory: memory.trim(),
        disk: disk.trim(),
        replicas: replicaCount,
        env: envs.filter((item) => item.key.trim() !== ""),
        // 每个非空行代表一个启动参数
        command: command
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean),
      })
      toast.success(`Pod ${name.trim()} 创建成功`)
      onOpenChange(false)
      onCreated()
    } catch (error) {
      toast.error((error as Error).message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>创建 Pod</DialogTitle>
          <DialogDescription>选择规格并填写镜像信息，创建后自动挂载 /data 数据盘。</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="pod-name">Pod 名称</Label>
            <Input
              id="pod-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如 my-dev-pod"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="pod-image">容器镜像</Label>
            <Input
              id="pod-image"
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="例如 ubuntu:22.04"
            />
          </div>

          <div className="space-y-1.5">
            <Label>规格预设</Label>
            <Select value={preset} onValueChange={applyPreset}>
              <SelectTrigger>
                <SelectValue placeholder="选择规格" />
              </SelectTrigger>
              <SelectContent>
                {SPEC_PRESETS.map((item) => (
                  <SelectItem key={item.label} value={item.label}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="pod-cpu">CPU（核）</Label>
              <Input id="pod-cpu" value={cpu} onChange={(e) => setCpu(e.target.value)} placeholder="2" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pod-memory">内存</Label>
              <Input id="pod-memory" value={memory} onChange={(e) => setMemory(e.target.value)} placeholder="4Gi" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="pod-disk">存储</Label>
              <Input id="pod-disk" value={disk} onChange={(e) => setDisk(e.target.value)} placeholder="5Gi" />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="pod-replicas">副本数</Label>
            <Input
              id="pod-replicas"
              type="number"
              min={0}
              value={replicas}
              onChange={(e) => setReplicas(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>环境变量</Label>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setEnvs((prev) => [...prev, { key: "", value: "" }])}
              >
                <Plus className="h-3.5 w-3.5" />
                添加
              </Button>
            </div>
            <div className="space-y-2">
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
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="text-slate-400"
                    onClick={() => setEnvs((prev) => prev.filter((_, i) => i !== index))}
                    disabled={envs.length === 1}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              ))}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="pod-command">启动命令（每行一个参数）</Label>
            <textarea
              id="pod-command"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              rows={3}
              placeholder={"留空则默认使用：sleep infinity"}
              className="w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            取消
          </Button>
          <Button onClick={handleSubmit} disabled={submitting}>
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
