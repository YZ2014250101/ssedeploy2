package k8s

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"ssedeploy/internal/model"
)

// 应用日志目录：固定在工作区内的 logs 子目录，与 IDE 文件树看到的是同一份数据。
const (
	// LogDirName 是日志目录名（相对工作区根）。
	LogDirName = "logs"
	// defaultAppLogLines 是默认拉取的末尾行数。
	defaultAppLogLines = 500
	// maxAppLogLines 是单次允许拉取的最大行数，防止一次读入过多内容。
	maxAppLogLines = 5000
	// maxAppLogBytes 是单次日志响应的字节上限（4MB），超出返回文件过大。
	maxAppLogBytes = 4 << 20
)

// AppLogDir 返回日志目录在容器内的绝对路径。
func AppLogDir() string { return path.Join(WorkspacePath, LogDirName) }

// AppLogFiles 列举日志目录下的普通文件（单层，按名称排序）。
// 目录不存在时返回空列表，不视为错误（多数 Pod 没有写日志文件）。
func (s *PodService) AppLogFiles(ctx context.Context, name string) ([]model.LogFile, error) {
	p, err := s.runningPod(ctx, name)
	if err != nil {
		return nil, err
	}

	// 只依赖 busybox 支持的 find 参数；逐行输出「名称 \t 字节数」，
	// 名称中可能包含空格，因此用 tab 分隔而不是空格。
	const script = `if [ ! -d "$1" ]; then exit 0; fi
cd "$1" || exit 0
find . -maxdepth 1 -type f | while IFS= read -r f; do
  n=${f#./}
  size=$(wc -c < "$f" | tr -d ' ')
  printf '%s\t%s\n' "$n" "$size"
done`

	out, err := s.execCapture(ctx, p, []string{"/bin/sh", "-c", script, "sh", AppLogDir()}, nil, maxListBytes)
	if err != nil {
		return nil, err
	}

	files := make([]model.LogFile, 0, 16)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		fileName, sizeText, ok := strings.Cut(line, "\t")
		if !ok || fileName == "" {
			continue
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
		if err != nil {
			continue
		}
		files = append(files, model.LogFile{Name: fileName, Size: size})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// AppLog 读取日志文件末尾 lines 行，同时返回文件绝对路径与大小。
// 单次 exec 完成「取大小 + tail」：首行为字节数，其余行为日志内容。
func (s *PodService) AppLog(ctx context.Context, name, fileName string, lines int) (*model.AppLog, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return nil, ErrInvalidName
	}
	// 只允许单层文件名：禁止 "/"、".." 等，杜绝读到日志目录以外的文件
	if err := validateEntryName(fileName); err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = defaultAppLogLines
	}
	if lines > maxAppLogLines {
		lines = maxAppLogLines
	}

	target := path.Join(AppLogDir(), fileName)

	p, err := s.runningPod(ctx, name)
	if err != nil {
		return nil, err
	}

	const script = `if [ ! -f "$1" ]; then echo NOTFOUND; exit 0; fi
wc -c < "$1" | tr -d ' '
tail -n "$2" "$1"`
	out, err := s.execCapture(
		ctx,
		p,
		[]string{"/bin/sh", "-c", script, "sh", target, strconv.Itoa(lines)},
		nil,
		maxAppLogBytes,
	)
	if err != nil {
		return nil, err
	}

	first, rest, _ := strings.Cut(out, "\n")
	if strings.TrimSpace(first) == "NOTFOUND" {
		return nil, ErrFileNotFound
	}
	size, err := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("读取日志文件失败: %w", err)
	}

	content := strings.TrimSuffix(rest, "\n")
	result := make([]string, 0, lines)
	if content != "" {
		for _, line := range strings.Split(content, "\n") {
			result = append(result, strings.TrimSuffix(line, "\r"))
		}
	}

	return &model.AppLog{
		FileName: fileName,
		FilePath: target,
		Size:     size,
		Lines:    result,
	}, nil
}
