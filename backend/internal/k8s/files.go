package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"ssedeploy/internal/model"
)

// ErrFileTooLarge 表示文件超出编辑器可承受的大小。
// Handler 层会把它映射为 HTTP 400，避免把超大文件塞进浏览器。
var ErrFileTooLarge = errors.New("文件过大，无法在编辑器中打开")

// ErrFileNotFound 表示工作区内文件不存在（映射为 HTTP 404）。
var ErrFileNotFound = errors.New("文件不存在")

// ErrOutsideWorkspace 表示请求路径试图跳出工作区根目录（映射为 HTTP 403）。
var ErrOutsideWorkspace = errors.New("不允许访问工作区外目录")

// ErrEntryExists 表示目标位置已存在同名文件或目录（映射为 HTTP 409）。
var ErrEntryExists = errors.New("同名文件或目录已存在")

// ErrInvalidName 表示文件/目录名称或路径不合法（映射为 HTTP 400）。
var ErrInvalidName = errors.New("名称不合法")

// ErrNotRegularFile 表示下载目标不是普通文件（映射为 HTTP 400）。
var ErrNotRegularFile = errors.New("只能下载文件，不能下载目录")

const (
	// maxReadBytes 单文件读取上限（1MB）
	maxReadBytes = 1 << 20
	// maxListBytes 目录列举输出上限（512KB），防止超大目录撑爆内存
	maxListBytes = 512 << 10
)

// workspaceRoot 把前端目录根映射成容器内的绝对路径。
// home -> /home/workspace；shared -> /home/workspace/shared。
func workspaceRoot(root string) (string, error) {
	switch root {
	case "", "home":
		return WorkspacePath, nil
	case "shared":
		return path.Join(WorkspacePath, SharedDirName), nil
	default:
		return "", fmt.Errorf("未知目录: %s", root)
	}
}

// cleanRel 归一化工作区相对路径，并拒绝任何越界访问。
//
// 规则（安全边界，必须严格遵守）：
//  1. 绝对路径一律视为越界（工作区内的路径只能以相对路径表达）；
//  2. 逐段解析 ".."，一旦越过工作区根目录立即返回 ErrOutsideWorkspace，
//     绝不静默截断，保证前端能收到明确提示；
//  3. 归一化结果再做一次前缀校验，防止特殊字符绕过。
func cleanRel(rel string) (string, error) {
	raw := strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "/") {
		return "", ErrOutsideWorkspace
	}
	if strings.ContainsRune(raw, 0) {
		return "", ErrInvalidName
	}

	// 逐段消费路径，栈深度为负即说明跳出了工作区根目录
	depth := 0
	for _, seg := range strings.Split(raw, "/") {
		switch seg {
		case "", ".":
			// 空段与当前目录不影响层级
		case "..":
			depth--
			if depth < 0 {
				return "", ErrOutsideWorkspace
			}
		default:
			depth++
		}
	}

	cleaned := path.Clean(raw)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrOutsideWorkspace
	}
	return cleaned, nil
}

// validateEntryName 校验新建/重命名的名称：只能是单层名称，不允许携带路径。
func validateEntryName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ErrInvalidName
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return ErrInvalidName
	}
	if len(name) > 255 {
		return ErrInvalidName
	}
	return nil
}

// shellQuote 用单引号包裹参数，内部单引号做转义，避免命令注入。
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// limitedBuffer 限制写入上限，超出时中断流，避免一次性读入超大输出。
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		return 0, ErrFileTooLarge
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// execCapture 在 Pod 内执行一次性命令并捕获标准输出（非 TTY）。
// 用于工作区文件列举、读取与写入，与终端共用同一个主容器。
func (s *PodService) execCapture(
	ctx context.Context,
	pod *corev1.Pod,
	cmd []string,
	stdin io.Reader,
	limit int,
) (string, error) {
	req := s.client.Kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(s.namespace).
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: logContainerName(pod), // 与终端/日志使用同一个主容器
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.client.Config, "POST", req.URL())
	if err != nil {
		return "", err
	}

	stdout := &limitedBuffer{limit: limit}
	stderr := &bytes.Buffer{}
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}); err != nil {
		if errors.Is(err, ErrFileTooLarge) {
			return "", ErrFileTooLarge
		}
		// 优先返回容器内的 stderr，便于定位（例如目录不存在、权限不足）
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("工作区命令执行失败: %s", msg)
	}
	return stdout.String(), nil
}

// ListFiles 递归列举工作区目录（最大 4 层），返回相对路径与类型。
// 会先 mkdir -p 目录根，保证 shared 等固定目录始终存在。
func (s *PodService) ListFiles(ctx context.Context, name, root, rel string) ([]model.IDEFileEntry, error) {
	base, err := workspaceRoot(root)
	if err != nil {
		return nil, err
	}
	sub, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	dir := base
	if sub != "" {
		dir = path.Join(base, sub)
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return nil, err
	}

	// 只依赖 busybox/coreutils 都支持的 find 参数：
	// 先用 while 读路径，再用 -d 判断目录/文件，避免依赖 find -printf。
	script := "mkdir -p " + shellQuote(dir) + " && cd " + shellQuote(dir) +
		` && find . -maxdepth 4 -mindepth 1 | while IFS= read -r p; do if [ -d "$p" ]; then echo "d:$p"; else echo "f:$p"; fi; done`
	out, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", script}, nil, maxListBytes)
	if err != nil {
		return nil, err
	}

	items := make([]model.IDEFileEntry, 0, 32)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}
		var entryType string
		switch line[:2] {
		case "d:":
			entryType = "dir"
		case "f:":
			entryType = "file"
		default:
			continue
		}
		// find 输出形如 ./pkg/util.go，去掉前导 "./"
		relPath := strings.TrimPrefix(line[2:], "./")
		if relPath == "" {
			continue
		}
		items = append(items, model.IDEFileEntry{Path: relPath, Type: entryType})
	}
	return items, nil
}

// ReadFile 读取工作区文件内容（上限 1MB）。
func (s *PodService) ReadFile(ctx context.Context, name, root, rel string) (string, error) {
	target, err := workspaceTarget(root, rel)
	if err != nil {
		return "", err
	}
	if rel == "" {
		return "", fmt.Errorf("请指定文件路径")
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return "", err
	}
	content, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", "cat " + shellQuote(target)}, nil, maxReadBytes)
	if err != nil {
		// 目录内不存在该文件时返回 404，而不是笼统的 500
		if strings.Contains(err.Error(), "No such file") {
			return "", ErrFileNotFound
		}
		return "", err
	}
	return content, nil
}

// WriteFile 把内容写入工作区文件，自动创建父目录。
// 内容通过 stdin 传给容器内的 cat，避免命令行长度限制与转义问题。
func (s *PodService) WriteFile(ctx context.Context, name, root, rel, content string) error {
	target, err := workspaceTarget(root, rel)
	if err != nil {
		return err
	}
	if rel == "" {
		return fmt.Errorf("请指定文件路径")
	}
	if len(content) > maxReadBytes {
		return ErrFileTooLarge
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}

	// $1 由位置参数传入，脚本负责创建父目录并写入
	const script = `mkdir -p "$(dirname "$1")" && cat > "$1"`
	_, err = s.execCapture(
		ctx,
		p,
		[]string{"/bin/sh", "-c", script, "sh", target},
		strings.NewReader(content),
		maxReadBytes,
	)
	return err
}

// CreateEntry 在指定父目录下新建文件或目录。
// parent 为相对目录根的路径（空表示根目录本身），entryName 必须是单层名称。
// 目标路径完全由后端基于工作区根拼接，前端无法借 ".." 在工作区外创建任何内容。
func (s *PodService) CreateEntry(ctx context.Context, name, root, parent, entryName string, isDir bool) error {
	if err := validateEntryName(entryName); err != nil {
		return err
	}
	dir, err := workspaceTarget(root, parent)
	if err != nil {
		return err
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}
	target := path.Join(dir, strings.TrimSpace(entryName))
	kind := "file"
	if isDir {
		kind = "dir"
	}

	// 已存在则输出 EXISTS（退出码保持 0，便于与真正的执行失败区分）
	const script = `if [ -e "$1" ]; then echo EXISTS; exit 0; fi
mkdir -p "$(dirname "$1")"
if [ "$2" = "dir" ]; then mkdir "$1"; else : > "$1"; fi
echo OK`
	out, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", script, "sh", target, kind}, nil, maxListBytes)
	if err != nil {
		return err
	}
	if strings.Contains(out, "EXISTS") {
		return ErrEntryExists
	}
	return nil
}

// RenameEntry 把工作区内的文件/目录重命名（仅允许同一目录下改名，避免借机跨目录）。
func (s *PodService) RenameEntry(ctx context.Context, name, root, rel, newName string) error {
	if err := validateEntryName(newName); err != nil {
		return err
	}
	if rel == "" {
		return ErrInvalidName // 不允许重命名工作区根目录
	}
	src, err := workspaceTarget(root, rel)
	if err != nil {
		return err
	}
	dst := path.Join(path.Dir(src), strings.TrimSpace(newName))
	if dst == src {
		return nil
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}

	const script = `if [ ! -e "$1" ]; then echo NOTFOUND; exit 0; fi
if [ -e "$2" ]; then echo EXISTS; exit 0; fi
mv "$1" "$2" && echo OK`
	out, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", script, "sh", src, dst}, nil, maxListBytes)
	if err != nil {
		return err
	}
	switch {
	case strings.Contains(out, "NOTFOUND"):
		return ErrFileNotFound
	case strings.Contains(out, "EXISTS"):
		return ErrEntryExists
	}
	return nil
}

// DeleteEntry 删除工作区内的文件或目录（目录递归删除），工作区根目录禁止删除。
func (s *PodService) DeleteEntry(ctx context.Context, name, root, rel string) error {
	if rel == "" {
		return ErrInvalidName
	}
	target, err := workspaceTarget(root, rel)
	if err != nil {
		return err
	}
	// 双重保险：目标必须是工作区内的子路径，且不能是工作区根/共享目录根
	if !strings.HasPrefix(target, WorkspacePath+"/") {
		return ErrOutsideWorkspace
	}
	if target == WorkspacePath || target == path.Join(WorkspacePath, SharedDirName) {
		return ErrInvalidName
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}

	const script = `if [ ! -e "$1" ]; then echo NOTFOUND; exit 0; fi
rm -rf "$1" && echo OK`
	out, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", script, "sh", target}, nil, maxListBytes)
	if err != nil {
		return err
	}
	if strings.Contains(out, "NOTFOUND") {
		return ErrFileNotFound
	}
	return nil
}

// OpenFile 返回工作区文件的内容流，用于浏览器下载。
// 先同步检查目标存在且为普通文件（保证出错时还能返回正确的 HTTP 状态码），
// 再用 exec 执行 cat 并把输出接到 io.Pipe，避免把大文件整体读进内存。
func (s *PodService) OpenFile(ctx context.Context, name, root, rel string) (io.ReadCloser, error) {
	if rel == "" {
		return nil, ErrInvalidName
	}
	target, err := workspaceTarget(root, rel)
	if err != nil {
		return nil, err
	}

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return nil, err
	}

	const probe = `if [ -f "$1" ]; then echo FILE; elif [ -e "$1" ]; then echo OTHER; else echo NONE; fi`
	out, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", probe, "sh", target}, nil, 64)
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(out) {
	case "NONE":
		return nil, ErrFileNotFound
	case "OTHER":
		return nil, ErrNotRegularFile
	}

	req := s.client.Kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(s.namespace).
		Name(p.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: logContainerName(p),
			Command:   []string{"/bin/sh", "-c", "cat " + shellQuote(target)},
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.client.Config, "POST", req.URL())
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		stderr := &bytes.Buffer{}
		if streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: writer,
			Stderr: stderr,
		}); streamErr != nil {
			_ = writer.CloseWithError(fmt.Errorf("读取文件失败: %s", strings.TrimSpace(stderr.String())))
		}
	}()
	return reader, nil
}

// workspaceTarget 返回工作区内文件的绝对路径，保证不会越出工作区。
func workspaceTarget(root, rel string) (string, error) {
	base, err := workspaceRoot(root)
	if err != nil {
		return "", err
	}
	sub, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if sub == "" {
		return base, nil
	}
	return path.Join(base, sub), nil
}
