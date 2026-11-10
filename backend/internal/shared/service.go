// Package shared 提供「全局共享目录」的文件管理能力。
//
// 共享目录在宿主机/容器内的路径由 SSD_SHARED_DIR 指定（默认 /shared），
// 平台内所有用户都能读写同一份数据，实现跨 Pod 全局共享。
//
// 安全约束（本包的核心）：
//  1. 前端传入的路径一律视为「相对共享根目录」的路径；
//  2. 拒绝绝对路径（除 /shared 前缀形式外），逐段解析 ".."，一旦越过根目录立即报错；
//  3. 解析软链接后再次校验真实路径必须仍在共享根目录内，防止通过软链接越权；
//  4. 新建/重命名只接受单层名称，彻底杜绝借名称拼接路径逃逸。
package shared

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 业务错误：Handler 层会把它们映射为对应的 HTTP 状态码。
var (
	// ErrOutsideShared 表示路径试图跳出共享根目录（HTTP 403）。
	ErrOutsideShared = errors.New("不允许访问共享目录外路径")
	// ErrInvalidName 表示文件/目录名称或路径不合法（HTTP 400）。
	ErrInvalidName = errors.New("名称不合法")
	// ErrNotFound 表示文件或目录不存在（HTTP 404）。
	ErrNotFound = errors.New("文件或目录不存在")
	// ErrEntryExists 表示目标位置已存在同名文件或目录（HTTP 409）。
	ErrEntryExists = errors.New("同名文件或目录已存在")
	// ErrNotRegularFile 表示下载目标既不是文件也不是目录（HTTP 400）。
	ErrNotRegularFile = errors.New("该路径不支持下载")
	// ErrRootLocked 表示对共享根目录执行了不允许的操作（HTTP 400）。
	ErrRootLocked = errors.New("共享根目录不允许重命名或删除")
)

const (
	// maxNameLen 单个文件/目录名称的最大长度。
	maxNameLen = 255
	// maxScanDepth 洞察统计的最大遍历深度，避免超深目录导致遍历时间不可控。
	maxScanDepth = 12
	// maxScanEntries 洞察统计的最大遍历条目数。
	maxScanEntries = 20000
)

// Entry 是文件树中的一个节点（相对共享根目录）。
type Entry struct {
	Name    string `json:"name"`    // 名称
	Path    string `json:"path"`    // 相对共享根的路径，根目录为空字符串
	Type    string `json:"type"`    // file / dir
	Size    int64  `json:"size"`    // 文件大小（目录为 0）
	ModTime int64  `json:"modTime"` // 最近修改时间（Unix 秒）
}

// Insight 是共享目录的 AI 洞察摘要（规则化统计生成，不依赖外部模型）。
type Insight struct {
	UpdatedAt   string   `json:"updatedAt"`   // 统计时间
	KeyStatus   string   `json:"keyStatus"`   // 关键状态
	Risks       []string `json:"risks"`       // 潜在风险
	Suggestions []string `json:"suggestions"` // 建议操作
	TotalFiles  int      `json:"totalFiles"`  // 文件总数
	TotalDirs   int      `json:"totalDirs"`   // 目录总数
	TotalSize   int64    `json:"totalSize"`   // 总占用字节数
}

// Service 是共享目录文件服务。
type Service struct {
	root string // 解析软链接后的真实根目录，所有路径校验都以它为基准
}

// New 创建共享目录服务：目录不存在时自动创建。
// 若指定目录不可创建（例如本地开发在 Windows 下没有 /shared 权限），
// 会退回到系统临时目录，保证服务仍能启动。
func New(dir string) (*Service, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "/shared"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("共享目录路径不合法: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		fallback := filepath.Join(os.TempDir(), "ssedeploy-shared")
		log.Printf("共享目录 %s 不可用（%v），回退到 %s", abs, err, fallback)
		if err := os.MkdirAll(fallback, 0o755); err != nil {
			return nil, fmt.Errorf("创建共享目录失败: %w", err)
		}
		abs = fallback
	}
	// 解析软链接，后续所有路径比较都基于真实路径
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("解析共享目录失败: %w", err)
	}
	return &Service{root: real}, nil
}

// Root 返回共享根目录的真实绝对路径。
func (s *Service) Root() string { return s.root }

// normalize 把前端传入的路径归一化为「相对共享根」的净路径（"" 表示根目录）。
// 任何试图跳出根目录的写法都会返回 ErrOutsideShared，而不是被静默截断。
func (s *Service) normalize(p string) (string, error) {
	raw := strings.TrimSpace(p)
	if strings.ContainsRune(raw, 0) {
		return "", ErrOutsideShared
	}
	// 统一分隔符，兼容 Windows 客户端传回的路径
	raw = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" || raw == "/" {
		return "", nil
	}
	// 允许前端直接传容器内的 /shared/xxx 形式
	switch {
	case raw == "/shared":
		return "", nil
	case strings.HasPrefix(raw, "/shared/"):
		raw = strings.TrimPrefix(raw, "/shared/")
	case strings.HasPrefix(raw, "/"):
		// 其它绝对路径一律拒绝，避免借绝对路径访问宿主机文件
		return "", ErrOutsideShared
	}

	// 逐段消费路径：深度为负说明已经跳出共享根目录
	depth := 0
	for _, seg := range strings.Split(raw, "/") {
		switch seg {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return "", ErrOutsideShared
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
		return "", ErrOutsideShared
	}
	return cleaned, nil
}

// validateName 校验新建/重命名的名称：只允许单层名称，不能携带路径分隔符。
func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ErrInvalidName
	}
	if strings.ContainsAny(name, `/\`+"\x00") {
		return ErrInvalidName
	}
	if len(name) > maxNameLen {
		return ErrInvalidName
	}
	return nil
}

// within 判断 target 是否等于 root 或位于 root 内。
func within(root, target string) bool {
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(filepath.Separator))
}

// resolve 返回共享根内的绝对路径，并做软链接逃逸检查。
// 对于尚不存在的目标路径，会先解析最近已存在祖先的真实路径，再接回剩余部分。
func (s *Service) resolve(rel string) (string, error) {
	target := s.root
	if rel != "" {
		target = filepath.Join(s.root, filepath.FromSlash(rel))
	}

	// 找到已存在的最深祖先，解析它的真实路径（软链接可能指向根目录之外）
	existing := target
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", ErrNotFound
	}
	if !within(s.root, real) {
		return "", ErrOutsideShared
	}
	if existing != target {
		suffix, relErr := filepath.Rel(existing, target)
		if relErr != nil {
			return "", ErrOutsideShared
		}
		target = filepath.Join(real, suffix)
	} else {
		target = real
	}
	if !within(s.root, target) {
		return "", ErrOutsideShared
	}
	return target, nil
}

// List 列举指定目录下的直接子项（不递归），目录在前、名称升序。
func (s *Service) List(p string) ([]Entry, error) {
	rel, err := s.normalize(p)
	if err != nil {
		return nil, err
	}
	abs, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("该路径不是目录")
	}

	items, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		entry := Entry{
			Name: item.Name(),
			Path: path.Join(rel, item.Name()),
			Type: "file",
		}
		if item.IsDir() {
			entry.Type = "dir"
		} else if info, statErr := item.Info(); statErr == nil {
			entry.Size = info.Size()
			entry.ModTime = info.ModTime().Unix()
		}
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return entries, nil
}

// sortEntries 目录优先，其次按名称排序。
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
}

// Mkdir 在 parent 目录下新建子目录。
func (s *Service) Mkdir(parent, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	rel, err := s.normalize(parent)
	if err != nil {
		return err
	}
	dir, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return ErrNotFound
	}
	target := filepath.Join(dir, strings.TrimSpace(name))
	if !within(s.root, target) {
		return ErrOutsideShared
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		return ErrEntryExists
	}
	return os.Mkdir(target, 0o755)
}

// SaveUpload 把上传的文件内容保存到 parent 目录下。
// 采用「临时文件 + 原子重命名」写入，避免上传中断留下半截文件。
func (s *Service) SaveUpload(parent, fileName string, reader io.Reader) error {
	if err := validateName(fileName); err != nil {
		return err
	}
	rel, err := s.normalize(parent)
	if err != nil {
		return err
	}
	dir, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return ErrNotFound
	}
	target := filepath.Join(dir, strings.TrimSpace(fileName))
	if !within(s.root, target) {
		return ErrOutsideShared
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		return ErrEntryExists
	}

	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// 失败路径兜底清理临时文件
		_ = os.Remove(tmpName)
	}()
	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	return nil
}

// Remove 删除文件或目录（目录递归删除），共享根目录禁止删除。
func (s *Service) Remove(p string) error {
	rel, err := s.normalize(p)
	if err != nil {
		return err
	}
	if rel == "" {
		return ErrRootLocked
	}
	abs, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if abs == s.root || !within(s.root, abs) {
		return ErrOutsideShared
	}
	if _, statErr := os.Lstat(abs); statErr != nil {
		if os.IsNotExist(statErr) {
			return ErrNotFound
		}
		return statErr
	}
	return os.RemoveAll(abs)
}

// Rename 在同一目录内重命名文件或目录。
func (s *Service) Rename(p, newName string) error {
	if err := validateName(newName); err != nil {
		return err
	}
	rel, err := s.normalize(p)
	if err != nil {
		return err
	}
	if rel == "" {
		return ErrRootLocked
	}
	src, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(src); statErr != nil {
		if os.IsNotExist(statErr) {
			return ErrNotFound
		}
		return statErr
	}
	dst := filepath.Join(filepath.Dir(src), strings.TrimSpace(newName))
	if !within(s.root, dst) {
		return ErrOutsideShared
	}
	if dst == src {
		return nil
	}
	if _, statErr := os.Lstat(dst); statErr == nil {
		return ErrEntryExists
	}
	return os.Rename(src, dst)
}

// OpenDownload 打开下载目标：普通文件直接返回文件流；
// 目录则在后台打包为 zip 流返回（前端统一按文件流下载）。
func (s *Service) OpenDownload(p string) (io.ReadCloser, string, bool, error) {
	rel, err := s.normalize(p)
	if err != nil {
		return nil, "", false, err
	}
	if rel == "" {
		return nil, "", false, ErrRootLocked
	}
	abs, err := s.resolve(rel)
	if err != nil {
		return nil, "", false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", false, ErrNotFound
		}
		return nil, "", false, err
	}

	if info.Mode().IsRegular() {
		file, openErr := os.Open(abs)
		if openErr != nil {
			return nil, "", false, openErr
		}
		return file, info.Name(), false, nil
	}
	if !info.IsDir() {
		return nil, "", false, ErrNotRegularFile
	}

	// 目录：边遍历边写入 zip，避免大目录一次性占用内存
	reader, writer := io.Pipe()
	go func() {
		zipWriter := zip.NewWriter(writer)
		walkErr := filepath.WalkDir(abs, func(current string, entry fs.DirEntry, err error) error {
			if err != nil {
				// 单个条目不可读时跳过，不中断整个压缩包
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil // 跳过软链接，防止打包到共享目录之外的内容
			}
			real, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil || !within(s.root, real) {
				return nil
			}
			relPath, relErr := filepath.Rel(abs, current)
			if relErr != nil || relPath == "." {
				return nil
			}
			name := filepath.ToSlash(relPath)
			if entry.IsDir() {
				_, createErr := zipWriter.Create(name + "/")
				return createErr
			}
			writerEntry, createErr := zipWriter.Create(name)
			if createErr != nil {
				return createErr
			}
			file, openErr := os.Open(real)
			if openErr != nil {
				return nil
			}
			_, copyErr := io.Copy(writerEntry, file)
			_ = file.Close()
			return copyErr
		})
		if walkErr != nil {
			_ = writer.CloseWithError(walkErr)
			return
		}
		if closeErr := zipWriter.Close(); closeErr != nil {
			_ = writer.CloseWithError(closeErr)
			return
		}
		_ = writer.Close()
	}()
	return reader, info.Name(), true, nil
}

// Insight 基于目录统计生成规则化的「AI 洞察」摘要。
// 不依赖外部大模型，保证接口在离线环境也可用。
func (s *Service) Insight() (Insight, error) {
	now := time.Now()
	insight := Insight{UpdatedAt: now.Format("2006-01-02 15:04:05")}

	var (
		totalSize  int64
		bigFiles   []string
		oldFiles   int
		emptyDirs  int
		maxDepth   int
		recent24h  int
		scanCount  int
		stopWalk   = errors.New("stop")
		cutoffTime = now.Add(-90 * 24 * time.Hour).Unix()
		recentTime = now.Add(-24 * time.Hour).Unix()
	)

	walkErr := filepath.WalkDir(s.root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if current != s.root && entry.Type()&os.ModeSymlink != 0 {
			return nil // 不统计软链接，避免越界
		}
		scanCount++
		if scanCount > maxScanEntries {
			return stopWalk
		}
		relPath, relErr := filepath.Rel(s.root, current)
		if relErr != nil || relPath == "." {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(relPath), "/") + 1
		if depth > maxDepth {
			maxDepth = depth
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if entry.IsDir() {
			insight.TotalDirs++
			if depth > maxScanDepth-2 {
				return filepath.SkipDir
			}
			children, readErr := os.ReadDir(current)
			if readErr == nil && len(children) == 0 {
				emptyDirs++
			}
			return nil
		}
		insight.TotalFiles++
		totalSize += info.Size()
		if info.Size() > 100<<20 {
			bigFiles = append(bigFiles, path.Join(filepath.ToSlash(relPath)))
		}
		if info.ModTime().Unix() < cutoffTime {
			oldFiles++
		}
		if info.ModTime().Unix() >= recentTime {
			recent24h++
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, stopWalk) {
		return insight, walkErr
	}
	insight.TotalSize = totalSize

	insight.KeyStatus = fmt.Sprintf(
		"共享目录共 %d 个文件、%d 个目录，占用 %s；最近 24 小时有 %d 个文件更新。",
		insight.TotalFiles, insight.TotalDirs, humanSize(totalSize), recent24h,
	)

	// 风险与建议均为规则化输出，便于后续替换为真实模型分析
	if len(bigFiles) > 0 {
		insight.Risks = append(insight.Risks,
			fmt.Sprintf("存在 %d 个超过 100MB 的大文件（如 %s），可能占用较多共享空间并拖慢访问。", len(bigFiles), bigFiles[0]))
	}
	if oldFiles > 0 {
		insight.Risks = append(insight.Risks,
			fmt.Sprintf("有 %d 个文件超过 90 天未更新，可能是历史产物或已废弃内容。", oldFiles))
	}
	if emptyDirs > 0 {
		insight.Risks = append(insight.Risks,
			fmt.Sprintf("发现 %d 个空目录，可能由误删操作遗留，影响目录结构清晰度。", emptyDirs))
	}
	if maxDepth > 6 {
		insight.Risks = append(insight.Risks,
			fmt.Sprintf("目录最大层级达到 %d 层，层级过深会增加维护与查找成本。", maxDepth))
	}
	if len(insight.Risks) == 0 {
		insight.Risks = append(insight.Risks, "未发现明显风险，目录结构与容量均处于正常范围。")
	}

	if len(bigFiles) > 0 {
		insight.Suggestions = append(insight.Suggestions, "对体积较大的文件进行评估，必要时压缩或迁移到对象存储。")
	}
	if emptyDirs > 0 {
		insight.Suggestions = append(insight.Suggestions, "清理无用的空目录，保持共享目录结构清晰。")
	}
	if oldFiles > 0 {
		insight.Suggestions = append(insight.Suggestions, "归档 90 天以上未更新的历史文件，减少误用与空间占用。")
	}
	if maxDepth > 6 {
		insight.Suggestions = append(insight.Suggestions, "将过深的目录层级扁平化，按业务模块重新规划目录结构。")
	}
	if totalSize > 5<<30 {
		insight.Suggestions = append(insight.Suggestions, "共享目录总占用已超过 5GB，建议定期清理并关注容量水位。")
	}
	if len(insight.Suggestions) == 0 {
		insight.Suggestions = append(insight.Suggestions, "保持当前使用习惯即可，建议定期查看共享目录使用情况。")
	}
	return insight, nil
}

// humanSize 把字节数格式化为易读字符串。
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(size)/float64(div), []string{"KB", "MB", "GB", "TB"}[exp])
}
