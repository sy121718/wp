package upload

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	uploadprovider "go_wp/pkg/upload/provider"

	"github.com/spf13/viper"
)

var (
	stateMu         sync.RWMutex
	runtimeMu       sync.RWMutex
	inited          bool
	configSource    *viper.Viper
	defaultProvider = "local"
	providers       = map[string]*providerEntry{}
	uploadRules     = validationRules{
		maxSize:           10 * 1024 * 1024,
		allowedExtensions: map[string]struct{}{},
		allowedMIMETypes:  map[string]struct{}{},
	}
)

type providerEntry struct {
	provider uploadprovider.Provider
	mu       sync.Mutex
	ready    bool
}

type validationRules struct {
	maxSize           int64
	allowedExtensions map[string]struct{}
	allowedMIMETypes  map[string]struct{}
}

type File = uploadprovider.File

type Request = uploadprovider.Request

type RuntimeConfig = uploadprovider.RuntimeConfig

type Result = uploadprovider.Result

type Client struct {
	provider string
	runtime  *RuntimeConfig
}

type Uploader struct {
	client  Client
	request Request
}

func Init(v *viper.Viper) error {
	if v == nil {
		return fmt.Errorf("upload 初始化配置为空")
	}

	if err := registerBuiltinProviders(); err != nil {
		return err
	}

	selected := normalizeProvider(v.GetString("upload.default_provider"))
	if selected == "" {
		selected = normalizeProvider(v.GetString("upload.provider"))
	}
	if selected == "" {
		return fmt.Errorf("upload.default_provider 不能为空")
	}

	stateMu.Lock()
	configSource = v
	defaultProvider = selected
	rules, err := parseValidationRules(v)
	if err != nil {
		stateMu.Unlock()
		return err
	}
	uploadRules = rules
	inited = true
	_, ok := providers[selected]
	stateMu.Unlock()

	if !ok {
		stateMu.Lock()
		inited = false
		stateMu.Unlock()
		return fmt.Errorf("默认 provider=%s 不存在", selected)
	}

	if err := ensureProviderReady(selected); err != nil {
		stateMu.Lock()
		inited = false
		stateMu.Unlock()
		return err
	}

	return nil
}

func Close() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	stateMu.RLock()
	if !inited {
		stateMu.RUnlock()
		return nil
	}
	entries := make([]*providerEntry, 0, len(providers))
	for _, entry := range providers {
		entries = append(entries, entry)
	}
	stateMu.RUnlock()

	var closeErr error
	for _, entry := range entries {
		entry.mu.Lock()
		if entry.ready {
			if err := entry.provider.Close(); err != nil {
				closeErr = errors.Join(closeErr, err)
			}
			entry.ready = false
		}
		entry.mu.Unlock()
	}

	stateMu.Lock()
	inited = false
	configSource = nil
	stateMu.Unlock()

	return closeErr
}

func IsInited() bool {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return inited
}

func Ready() error {
	if !IsInited() {
		return fmt.Errorf("上传组件未初始化")
	}
	return nil
}

func Register(provider uploadprovider.Provider) error {
	if provider == nil {
		return fmt.Errorf("上传 provider 不能为空")
	}

	name := normalizeProvider(provider.Name())
	if name == "" {
		return fmt.Errorf("上传 provider 名称不能为空")
	}

	stateMu.Lock()
	providers[name] = &providerEntry{provider: provider}
	shouldInit := inited && configSource != nil
	stateMu.Unlock()

	if shouldInit {
		if err := ensureProviderReady(name); err != nil {
			return fmt.Errorf("初始化 provider=%s 失败: %w", name, err)
		}
	}

	return nil
}

func Providers() []string {
	stateMu.RLock()
	defer stateMu.RUnlock()

	result := make([]string, 0, len(providers))
	for name := range providers {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func Upload(ctx context.Context, file File, req Request) (Result, error) {
	return uploadWithProvider(ctx, "", RuntimeConfig{}, file, req)
}

func UploadWithConfig(ctx context.Context, runtime RuntimeConfig, file File, req Request) (Result, error) {
	return uploadWithProvider(ctx, runtime.Provider, runtime, file, req)
}

func Use(provider string) Client {
	return Client{provider: normalizeProvider(provider)}
}

func UseCfg(runtime RuntimeConfig) Client {
	cfg := runtime
	return Client{provider: normalizeProvider(cfg.Provider), runtime: &cfg}
}

func NewUploader(provider string, request Request) Uploader {
	return Uploader{
		client:  Use(provider),
		request: request,
	}
}

func NewUploaderWithConfig(runtime RuntimeConfig, request Request) Uploader {
	return Uploader{
		client:  UseCfg(runtime),
		request: request,
	}
}

func (c Client) Upload(ctx context.Context, file File, req Request) (Result, error) {
	if c.runtime != nil {
		return uploadWithProvider(ctx, c.provider, *c.runtime, file, req)
	}
	return uploadWithProvider(ctx, c.provider, RuntimeConfig{}, file, req)
}

func (u Uploader) Upload(ctx context.Context, file File) (Result, error) {
	return u.client.Upload(ctx, file, u.request)
}

func uploadWithProvider(ctx context.Context, providerName string, runtime RuntimeConfig, file File, req Request) (Result, error) {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()

	provider, name, err := getProvider(providerName)
	if err != nil {
		return Result{}, err
	}

	runtime.Provider = name
	if name != "local" && !hasOnlineRuntimeConfig(runtime) {
		return Result{}, fmt.Errorf("上传配置缺失")
	}
	if err := validateFile(file); err != nil {
		return Result{}, err
	}

	result, err := provider.Upload(ctx, runtime, file, req)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = name
	}

	return result, nil
}

func getProvider(providerName string) (uploadprovider.Provider, string, error) {
	stateMu.RLock()
	initialized := inited
	name := normalizeProvider(providerName)
	if name == "" {
		name = defaultProvider
	}
	entry, ok := providers[name]
	stateMu.RUnlock()

	if !initialized {
		return nil, "", fmt.Errorf("上传组件未初始化")
	}
	if !ok {
		return nil, "", fmt.Errorf("provider=%s 不存在", name)
	}
	if err := ensureProviderReady(name); err != nil {
		return nil, "", err
	}

	return entry.provider, name, nil
}

func ensureProviderReady(providerName string) error {
	stateMu.RLock()
	initialized := inited
	cfg := configSource
	entry, ok := providers[providerName]
	stateMu.RUnlock()

	if !initialized {
		return fmt.Errorf("上传组件未初始化")
	}
	if !ok {
		return fmt.Errorf("provider=%s 不存在", providerName)
	}
	if cfg == nil {
		return fmt.Errorf("上传配置无效")
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.ready {
		return nil
	}

	if err := entry.provider.Init(cfg); err != nil {
		return fmt.Errorf("初始化 provider=%s 失败: %w", providerName, err)
	}
	entry.ready = true
	return nil
}

func hasOnlineRuntimeConfig(cfg RuntimeConfig) bool {
	if strings.TrimSpace(cfg.Endpoint) != "" {
		return true
	}
	if strings.TrimSpace(cfg.Bucket) != "" {
		return true
	}
	if strings.TrimSpace(cfg.Region) != "" {
		return true
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return true
	}
	if strings.TrimSpace(cfg.AccessKey) != "" {
		return true
	}
	if strings.TrimSpace(cfg.SecretKey) != "" {
		return true
	}
	return len(cfg.Extra) > 0
}

func normalizeProvider(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func parseValidationRules(v *viper.Viper) (validationRules, error) {
	rules := validationRules{
		maxSize:           10 * 1024 * 1024,
		allowedExtensions: map[string]struct{}{},
		allowedMIMETypes:  map[string]struct{}{},
	}
	if v == nil {
		return rules, nil
	}

	if raw := strings.TrimSpace(v.GetString("upload.max_size")); raw != "" {
		size, err := parseByteSize(raw)
		if err != nil {
			return validationRules{}, fmt.Errorf("upload.max_size 配置无效: %w", err)
		}
		rules.maxSize = size
	}

	for _, ext := range v.GetStringSlice("upload.allowed_extensions") {
		normalized := strings.ToLower(strings.TrimSpace(ext))
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, ".") {
			normalized = "." + normalized
		}
		rules.allowedExtensions[normalized] = struct{}{}
	}

	for _, mimeType := range v.GetStringSlice("upload.allowed_mime_types") {
		normalized := strings.ToLower(strings.TrimSpace(mimeType))
		if normalized == "" {
			continue
		}
		rules.allowedMIMETypes[normalized] = struct{}{}
	}

	return rules, nil
}

func validateFile(file File) error {
	if file.Size > 0 && uploadRules.maxSize > 0 && file.Size > uploadRules.maxSize {
		return fmt.Errorf("上传文件大小超限: max=%d current=%d", uploadRules.maxSize, file.Size)
	}

	if len(uploadRules.allowedExtensions) > 0 {
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(file.Filename)))
		if _, ok := uploadRules.allowedExtensions[ext]; !ok {
			return fmt.Errorf("上传扩展名不允许: %s", ext)
		}
	}

	if len(uploadRules.allowedMIMETypes) > 0 {
		contentType := strings.ToLower(strings.TrimSpace(file.ContentType))
		if _, ok := uploadRules.allowedMIMETypes[contentType]; !ok {
			return fmt.Errorf("上传 MIME 类型不允许: %s", file.ContentType)
		}
	}

	return nil
}

func parseByteSize(raw string) (int64, error) {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	units := []struct {
		suffix string
		scale  int64
	}{
		{suffix: "KB", scale: 1024},
		{suffix: "MB", scale: 1024 * 1024},
		{suffix: "GB", scale: 1024 * 1024 * 1024},
		{suffix: "B", scale: 1},
	}

	for _, unit := range units {
		if strings.HasSuffix(normalized, unit.suffix) {
			text := strings.TrimSpace(strings.TrimSuffix(normalized, unit.suffix))
			var value int64
			_, err := fmt.Sscanf(text, "%d", &value)
			if err != nil {
				return 0, err
			}
			if value <= 0 {
				return 0, fmt.Errorf("值必须大于 0")
			}
			return value * unit.scale, nil
		}
	}

	var value int64
	_, err := fmt.Sscanf(normalized, "%d", &value)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("值必须大于 0")
	}

	return value, nil
}

func registerBuiltinProviders() error {
	if err := Register(uploadprovider.NewLocalProvider()); err != nil {
		return err
	}
	if err := Register(uploadprovider.NewQiniuProvider()); err != nil {
		return err
	}
	return nil
}
