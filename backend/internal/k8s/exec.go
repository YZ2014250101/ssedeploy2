package k8s

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// shellInitPath 是写入容器内的 shell 初始化脚本路径（不在工作区，避免污染文件树）。
const shellInitPath = "/tmp/.ssedeploy_shellrc"

// workspaceShellInit 是终端 shell 的初始化脚本：
// 1. 重写 cd 命令，把当前目录严格限制在工作区根目录内；
// 2. 为常见写文件命令增加路径校验，禁止通过相对 ".." 或工作区外绝对路径创建内容。
// __WORKSPACE__ 会在运行时替换成实际的工作区绝对路径。
const workspaceShellInit = `# SSEDeploy: 将 shell 的路径操作限制在工作区根目录内
__ssedeploy_cd() {
  __ssedeploy_target="$1"
  if [ -z "$__ssedeploy_target" ]; then
    __ssedeploy_target="__WORKSPACE__"
  else
    case "$__ssedeploy_target" in
      /*) ;;
      *) __ssedeploy_target="$PWD/$__ssedeploy_target" ;;
    esac
  fi
  # 在子 shell 中解析真实物理路径，失败说明目录不存在
  __ssedeploy_resolved=$(command cd -P "$__ssedeploy_target" 2>/dev/null && pwd)
  if [ -z "$__ssedeploy_resolved" ]; then
    echo "目录不存在: $__ssedeploy_target" >&2
    return 1
  fi
  case "$__ssedeploy_resolved" in
    "__WORKSPACE__"|"__WORKSPACE__"/*)
      command cd -P "$__ssedeploy_resolved"
      ;;
    *)
      echo "不允许访问工作区外目录" >&2
      return 1
      ;;
  esac
}
cd() { __ssedeploy_cd "$@"; }

# 路径校验：拒绝含 ".." 的路径段与工作区外的绝对路径
__ssedeploy_guard_paths() {
  for __ssedeploy_p in "$@"; do
    case "$__ssedeploy_p" in
      ""|-|-*) continue ;;
    esac
    case "$__ssedeploy_p" in
      ..|../*|*/..|*/../*)
        echo "不允许访问工作区外目录" >&2
        return 1
        ;;
    esac
    case "$__ssedeploy_p" in
      /*)
        case "$__ssedeploy_p" in
          "__WORKSPACE__"|"__WORKSPACE__"/*) ;;
          *)
            echo "不允许访问工作区外目录" >&2
            return 1
            ;;
        esac
        ;;
    esac
  done
  return 0
}
mkdir() { __ssedeploy_guard_paths "$@" && command mkdir "$@"; }
touch() { __ssedeploy_guard_paths "$@" && command touch "$@"; }
rm() { __ssedeploy_guard_paths "$@" && command rm "$@"; }
rmdir() { __ssedeploy_guard_paths "$@" && command rmdir "$@"; }
cp() { __ssedeploy_guard_paths "$@" && command cp "$@"; }
mv() { __ssedeploy_guard_paths "$@" && command mv "$@"; }
ln() { __ssedeploy_guard_paths "$@" && command ln "$@"; }
tee() { __ssedeploy_guard_paths "$@" && command tee "$@"; }
`

// normalizeShell 把前端传入的 shell 收敛到白名单，
// 避免把查询参数直接拼进 shell 命令造成命令注入。
func normalizeShell(shell string) string {
	switch shell {
	case "/bin/bash", "bash":
		return "/bin/bash"
	case "/bin/ash":
		return "/bin/ash"
	case "/bin/zsh":
		return "/bin/zsh"
	default:
		return "/bin/sh"
	}
}

// Exec 通过 SPDY 连接到 Pod 内 shell，实现 WebSSH。
// stdin/stdout/stderr 由 WebSocket 适配器提供，sizeQ 用于窗口尺寸同步。
func (s *PodService) Exec(
	ctx context.Context,
	name, shell string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	tty bool,
	sizeQ remotecommand.TerminalSizeQueue,
) error {
	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}
	sh := normalizeShell(shell)

	// 1. 写入 shell 初始化脚本：其中把 cd 限制在工作区根目录内，
	//    保证 `cd ..` 到达 /home/workspace 后不再向上跳转，也无法进入工作区外。
	initScript := strings.ReplaceAll(workspaceShellInit, "__WORKSPACE__", WorkspacePath)
	writeInit := "cat > " + shellQuote(shellInitPath) + " <<'SSEDEPLOY_SHELL_INIT'\n" +
		initScript + "SSEDEPLOY_SHELL_INIT\n"

	// 2. 显式切到工作区后再启动交互 shell；
	//    即使容器的 WorkingDir 未生效（历史 Pod 或运行时差异），终端也落在工作区。
	startCmd := "cd " + shellQuote(WorkspacePath) + " 2>/dev/null || true\n"
	if filepath.Base(sh) == "bash" {
		// bash 的交互式初始化走 --rcfile，不读取 ENV
		startCmd += "exec " + sh + " --rcfile " + shellQuote(shellInitPath) + " -i"
	} else {
		// POSIX sh / ash / dash 的交互式 shell 会读取 ENV 指向的脚本
		startCmd += "ENV=" + shellQuote(shellInitPath) + "; export ENV\n" +
			"exec " + sh + " -i"
	}

	// 构造 exec 子资源请求
	req := s.client.Kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(s.namespace).
		Name(p.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: logContainerName(p),
			Command:   []string{sh, "-c", writeInit + startCmd},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       tty,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.client.Config, "POST", req.URL())
	if err != nil {
		return err
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               tty,
		TerminalSizeQueue: sizeQ,
	})
}
