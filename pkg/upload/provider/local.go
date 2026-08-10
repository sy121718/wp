package uploadprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		// upload.base_url 提供站点完整域名（如 http://localhost:8080），
		// 拼接 /storage 作为资源前缀；未配置时保持相对路径 /storage
		if siteURL := strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(v.GetString("upload.base_url")), "\\", "/"), "/"); siteURL != "" {
			p.baseURL = siteURL + p.baseURL
		}
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
	p.mu.RUnlock()

	objectKey, err := buildObjectKey(file.Filename, req)
	if err != nil {
		return Result{}, err
	}

	targetPath := filepath.Join(rootDir, filepath.FromSlash(objectKey))
	targetPath = filepath.Clean(targetPath)

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("创建上传目录失败: %w", err)
	}

	fd, err := os.Create(targetPath)
	if err != nil {
		return Result{}, fmt.Errorf("创建上传文件失败: %w", err)
	}
	defer fd.Close()

	written, err := io.Copy(fd, file.Reader)
	if err != nil {
		return Result{}, fmt.Errorf("写入上传文件失败: %w", err)
	}

	return Result{
		Provider: localName,
		Key:      strings.ReplaceAll(objectKey, "\\", "/"),
		URL:      buildURL(baseURL, objectKey),
		Size:     written,
	}, nil
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
	return replacer.Replace(raw)
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
