package uploadprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/viper"
)

const (
	localName       string = "local"
	defaultLocalDir string = "public/storage"
	defaultLocalURL string = "/storage"
)

type localProvider struct {
	mu      sync.RWMutex
	rootDir string
	baseURL string
	// maxSize 上传大小上限（字节），0 表示不限制。
	// 从 upload.max_size 读取，用于流式限制与超限自清理（不信任调用方声明的 Size）。
	maxSize int64
	inited  bool
}

func NewLocalProvider() Provider {
	return &localProvider{}
}

func (p *localProvider) Name() string {
	return localName
}

func (p *localProvider) Init(v *viper.Viper) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.inited {
		return nil
	}

	p.rootDir = filepath.Clean(defaultLocalDir)
	p.baseURL = strings.TrimRight(strings.ReplaceAll(defaultLocalURL, "\\", "/"), "/")
	if v != nil {
		// upload.local_dir 可覆盖存储根目录（默认 public/storage），
		// 测试环境用它指向临时目录，避免污染仓库目录。
		if dir := strings.TrimSpace(v.GetString("upload.local_dir")); dir != "" {
			p.rootDir = filepath.Clean(dir)
		}
		// upload.base_url 提供站点完整域名（如 http://localhost:8080），
		// 拼接 /storage 作为资源前缀；未配置时保持相对路径 /storage
		if siteURL := strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(v.GetString("upload.base_url")), "\\", "/"), "/"); siteURL != "" {
			p.baseURL = siteURL + p.baseURL
		}
		// 大小上限：GetSizeInBytes 支持 "10MB"/"1GB" 等可读格式；解析失败返回 0（不限制）。
		p.maxSize = int64(v.GetSizeInBytes("upload.max_size"))
	}
	p.inited = true
	return nil
}

func (p *localProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inited = false
	return nil
}

func (p *localProvider) Upload(_ context.Context, _ RuntimeConfig, file File, req Request) (Result, error) {
	if file.Reader == nil {
		return Result{}, fmt.Errorf("上传文件为空")
	}
	if strings.TrimSpace(file.Filename) == "" && strings.TrimSpace(req.ObjectKey) == "" {
		return Result{}, fmt.Errorf("上传文件名缺失")
	}

	p.mu.RLock()
	if !p.inited {
		p.mu.RUnlock()
		return Result{}, fmt.Errorf("上传组件未初始化")
	}
	rootDir := p.rootDir
	baseURL := p.baseURL
	maxSize := p.maxSize
	p.mu.RUnlock()

	objectKey, err := buildObjectKey(file.Filename, req)
	if err != nil {
		return Result{}, err
	}

	targetPath := filepath.Join(rootDir, filepath.FromSlash(objectKey))
	targetPath = filepath.Clean(targetPath)
	if rel, err := filepath.Rel(rootDir, targetPath); err != nil || strings.HasPrefix(rel, "..") {
		return Result{}, fmt.Errorf("上传路径越界")
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("创建上传目录失败: %w", err)
	}

	// O_EXCL 防覆盖（PreserveName 同名二次上传直接失败而非截断覆盖），
	// O_NOFOLLOW 防 symlink 覆盖（攻击者预置软链指向受害文件时 open 失败）。
	// 注意：O_NOFOLLOW 只保护最终路径分量，中间目录仍跟随 symlink——
	// 能写入 storage 下中间目录的攻击者已具备更高权限，超出本层防御范围。
	fd, err := openExclusive(targetPath)
	if err != nil && errors.Is(err, os.ErrExist) && strings.TrimSpace(req.ObjectKey) != "" {
		// 调用方指定了键却被占用：系统分配的键（如媒体库的 <id>.<ext>）用完了
		// 「自己分配的空间」，磁盘上那份是别的世代的遗留文件——库表重置而
		// storage 目录长期累积，两套命名空间会撞在一起。此时既不能覆盖
		// （O_EXCL 的初衷），也不该让整次上传失败：改写成相邻的带随机后缀键，
		// 把实际落盘键与 URL 返回给调用方。PreserveName 路径不吃这条分支，
		// 「同名二次上传失败」的语义原样保留。
		objectKey, targetPath, err = collisionKey(rootDir, objectKey)
		if err != nil {
			return Result{}, err
		}
		fd, err = openExclusive(targetPath)
	}
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return Result{}, fmt.Errorf("目标文件已存在")
		}
		return Result{}, fmt.Errorf("创建上传文件失败: %w", err)
	}
	defer fd.Close()

	// 流式大小限制：不信任 file.Size（调用方可能谎报 0），最多写入 maxSize+1 字节，
	// 写入后按实际字节数判定超限并删除残留文件。
	reader := io.Reader(file.Reader)
	if maxSize > 0 {
		reader = io.LimitReader(file.Reader, maxSize+1)
	}
	written, err := io.Copy(fd, reader)
	if err != nil {
		_ = os.Remove(targetPath)
		return Result{}, fmt.Errorf("写入上传文件失败: %w", err)
	}
	if maxSize > 0 && written > maxSize {
		_ = os.Remove(targetPath)
		return Result{}, fmt.Errorf("上传文件大小超限: max=%d current=%d", maxSize, written)
	}

	return Result{
		Provider: localName,
		Key:      strings.ReplaceAll(objectKey, "\\", "/"),
		URL:      buildURL(baseURL, objectKey),
		Size:     written,
	}, nil
}

// openExclusive 以「不覆盖、不跟随软链」的方式打开目标文件。
func openExclusive(target string) (*os.File, error) {
	return os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
}

// collisionKey 在目标键已被磁盘占用时，为同一目录生成一个带随机后缀的相邻键。
//
// 只给「调用方显式指定键」的路径用：那种键由程序分配（媒体库 <id>.<ext>），
// 撞上遗留文件意味着两套主键世代错位，换一个即可；而 PreserveName 的同名语义
// 必须保持「失败」，否则用户会拿到一份被改名的新文件却以为覆盖了旧的。
func collisionKey(rootDir, objectKey string) (string, string, error) {
	dir := path.Dir(objectKey)
	ext := path.Ext(objectKey)
	base := strings.TrimSuffix(path.Base(objectKey), ext)
	for i := 0; i < 8; i++ {
		randPart, err := randomHex(3)
		if err != nil {
			return "", "", fmt.Errorf("生成随机文件名失败: %w", err)
		}
		key := base + "-" + randPart + ext
		if dir != "." && dir != "/" {
			key = dir + "/" + key
		}
		target := filepath.Clean(filepath.Join(rootDir, filepath.FromSlash(key)))
		if rel, err := filepath.Rel(rootDir, target); err != nil || strings.HasPrefix(rel, "..") {
			return "", "", fmt.Errorf("上传路径越界")
		}
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			return key, target, nil
		}
	}
	return "", "", fmt.Errorf("目标文件已存在")
}

func buildObjectKey(filename string, req Request) (string, error) {
	if key := strings.TrimSpace(req.ObjectKey); key != "" {
		return sanitizeObjectKey(key), nil
	}

	ext := strings.ToLower(filepath.Ext(filename))
	route := sanitizeDir(req.Route)
	dir := sanitizeDir(req.Directory)
	if route != "" {
		if dir == "" {
			dir = route
		} else {
			dir = route + "/" + dir
		}
	}

	name := ""
	if req.PreserveName {
		name = sanitizeFilename(strings.TrimSuffix(filepath.Base(filename), ext))
	}
	if name == "" {
		randPart, err := randomHex(6)
		if err != nil {
			return "", fmt.Errorf("生成随机文件名失败: %w", err)
		}
		name = fmt.Sprintf("%d_%s", time.Now().UnixNano(), randPart)
	}

	if ext == "" {
		if dir == "" {
			return name, nil
		}
		return dir + "/" + name, nil
	}

	if dir == "" {
		return name + ext, nil
	}
	return dir + "/" + name + ext, nil
}

func buildURL(baseURL string, objectKey string) string {
	key := strings.ReplaceAll(strings.TrimLeft(objectKey, "/"), "\\", "/")
	if baseURL == "" {
		return "/" + key
	}
	return strings.TrimRight(baseURL, "/") + "/" + key
}

func sanitizeDir(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	raw = strings.ReplaceAll(raw, "|", "/")
	raw = strings.ReplaceAll(raw, ",", "/")
	if raw == "" {
		return ""
	}
	raw = strings.Trim(raw, "/")
	parts := make([]string, 0)
	for _, part := range strings.Split(raw, "/") {
		part = sanitizeFilename(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

func sanitizeObjectKey(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	raw = strings.TrimLeft(raw, "/")
	if raw == "" {
		return ""
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(raw, "/") {
		part = sanitizeFilename(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

func sanitizeFilename(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw == "." || raw == ".." {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	name := replacer.Replace(raw)
	// 尾随点/空格：Windows 与多数对象存储会静默裁剪，造成保留名命中或
	// 扩展名伪装（如 "shell.txt. " 落盘为 "shell.txt"），统一去掉。
	name = strings.TrimRight(name, ". ")
	// Windows 保留设备名（CON/PRN/AUX/NUL/COM1-9/LPT1-9，含带扩展名形式）：
	// 在 Windows 落盘或同步到对象存储时会被系统拦截或语义错乱，前缀下划线转义。
	if isWindowsReservedName(name) {
		name = "_" + name
	}
	return name
}

// isWindowsReservedName 判断文件名基名（去掉扩展名后）是否为 Windows 保留设备名。
// 判定按扩展名前的基名进行，覆盖 "CON.txt"、"COM1.log" 等变体。
func isWindowsReservedName(name string) bool {
	base := name
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	upper := strings.ToUpper(strings.TrimSpace(base))
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(upper) != 4 {
		return false
	}
	if (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) &&
		upper[3] >= '1' && upper[3] <= '9' {
		return true
	}
	return false
}

func randomHex(byteLen int) (string, error) {
	if byteLen <= 0 {
		byteLen = 6
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
