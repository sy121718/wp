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
)

type localProvider struct {
	mu      sync.RWMutex
	rootDir string
	baseURL string

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
	baseURL, err := MediaBaseURL(v)
	if err != nil {
		return err
	}

	p.rootDir = filepath.Clean(defaultLocalDir)
	p.baseURL = baseURL
	if v != nil {

		if dir := strings.TrimSpace(v.GetString("upload.local_dir")); dir != "" {
			p.rootDir = filepath.Clean(dir)
		}

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

	fd, err := openExclusive(targetPath)
	if err != nil && errors.Is(err, os.ErrExist) && strings.TrimSpace(req.ObjectKey) != "" {

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

func openExclusive(target string) (*os.File, error) {
	return os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
}

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

	name = strings.TrimRight(name, ". ")

	if isWindowsReservedName(name) {
		name = "_" + name
	}
	return name
}

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
