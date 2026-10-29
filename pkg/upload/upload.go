package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"

	uploadprovider "go_wp/pkg/upload/provider"

	"github.com/spf13/viper"
)

// localProviderDefaultDir 本地存储默认根目录，与 provider 包 defaultLocalDir 同值
// （provider 包未导出该常量，这里同步维护，LocalDir 未初始化兜底用）。
const localProviderDefaultDir = "public/storage"

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
	// mediaBaseURL 是初始化时校验过的完整媒体基址。URL 由存储键按当前配置派生。
	mediaBaseURL string
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
	baseURL, err := uploadprovider.MediaBaseURL(v)
	if err != nil {
		return err
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
	mediaBaseURL = baseURL
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
	mediaBaseURL = ""
	stateMu.Unlock()

	return closeErr
}

func IsInited() bool {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return inited
}

// LocalDir 返回本地存储根目录（local provider 的落盘根），供服务端生成衍生文件
// （如媒体变体）直接写盘时定位存储路径——变体是受信的服务端产物，不经上传校验入口。
// 与 provider/local.go Init 的解析逻辑保持一致：upload.local_dir 覆盖，默认 public/storage。
// 未初始化（如单测环境）时返回默认值，避免空指针。
func LocalDir() string {
	stateMu.RLock()
	v := configSource
	stateMu.RUnlock()
	if v != nil {
		if dir := strings.TrimSpace(v.GetString("upload.local_dir")); dir != "" {
			return filepath.Clean(dir)
		}
	}
	return filepath.Clean(localProviderDefaultDir)
}

// BaseURL 媒体资源的对外前缀（不含末尾斜杠）。
// 未初始化时为空；调用方不得回退成相对路径。
func BaseURL() string {
	stateMu.RLock()
	base := mediaBaseURL
	stateMu.RUnlock()
	return strings.TrimRight(base, "/")
}

// StorageKey 从本地存储键或可信媒体地址提取相对 storage 根的键。
// 兼容 key、storage/key、/storage/key；完整地址仅接受当前配置的域名和路径前缀。
// 任意外链即使包含 /storage/ 也不视为本地资产；历史附件的迁移由已知 file_path 决定。
// 危险协议、越界路径或空值返回空串。
func StorageKey(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.User != nil || strings.Contains(u, "\\") {
		return ""
	}
	if parsed.IsAbs() || parsed.Host != "" {
		base, baseErr := url.Parse(BaseURL())
		if baseErr != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) || parsed.Scheme != base.Scheme {
			return ""
		}
		prefix := strings.TrimRight(base.Path, "/") + "/"
		if !strings.HasPrefix(parsed.Path, prefix) {
			return ""
		}
		u = strings.TrimPrefix(parsed.Path, prefix)
	} else {
		u = parsed.Path
	}
	key := strings.TrimLeft(u, "/")
	if rest, ok := strings.CutPrefix(key, "storage/"); ok {
		key = rest
	}
	if key == "" || key == ".." || strings.HasPrefix(key, "../") || path.Clean(key) != key || strings.Contains(key, "\x00") {
		return ""
	}
	return key
}

// StorageURL 把存储标识拼成对外可用的媒体 URL。
//
// 已知本地资产应传 file_path 存储键，以便换域名后按新配置派生。
// 任意外部 HTTP(S) 地址保留原域名；危险协议与无法归一的值返回空。
func StorageURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.User != nil || strings.ContainsAny(u, "\\\x00\r\n\t") {
		return ""
	}
	if parsed.IsAbs() || parsed.Host != "" {
		if parsed.Scheme == "" && parsed.Host != "" {
			parsed.Scheme = "https"
		}
		if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return ""
		}
		return parsed.String()
	}
	if strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "/storage/") {
		return ""
	}
	if key := StorageKey(u); key != "" && BaseURL() != "" {
		base, _ := url.Parse(BaseURL())
		base.Path += "/" + key
		base.RawQuery = parsed.RawQuery
		base.Fragment = parsed.Fragment
		return base.String()
	}
	return ""
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

// Upload 按当前组件配置上传（生产路径：provider 由 upload.default_provider 决定，
// 上传前经 config.InitComponents 的 Init 完成就绪检查）。
func Upload(ctx context.Context, file File, req Request) (Result, error) {
	return uploadWithProvider(ctx, "", RuntimeConfig{}, file, req)
}

// Use 按 provider 名构造 Client（同一进程内按调用点选 provider 的入口）。
func Use(provider string) Client {
	return Client{provider: normalizeProvider(provider)}
}

// UseCfg 按一份显式运行时配置构造 Client（provider 名与配置一起给出）。
//
// 与 Upload / Use 的分工：那条路径的 provider 配置来自全局 Init(cfg)；
// 本入口把配置交给调用方，供「同一进程内用不同 provider 配置上传」的场景使用。
func UseCfg(runtime RuntimeConfig) Client {
	cfg := runtime
	return Client{provider: normalizeProvider(cfg.Provider), runtime: &cfg}
}

// NewUploader 构造一个绑定了默认 Request 的上传器（provider 名 + 每次调用的 Request）。
func NewUploader(provider string, request Request) Uploader {
	return Uploader{
		client:  Use(provider),
		request: request,
	}
}

// Client 是「provider 维度」的上传门面（见 Use / UseCfg）。
func (c Client) Upload(ctx context.Context, file File, req Request) (Result, error) {
	if c.runtime != nil {
		return uploadWithProvider(ctx, c.provider, *c.runtime, file, req)
	}
	return uploadWithProvider(ctx, c.provider, RuntimeConfig{}, file, req)
}

// Uploader 是「provider + 默认 Request」维度的门面（见 NewUploader）。
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
	// 魔数嗅探：读取前 512 字节检测真实内容类型，拒绝伪装成图片/pdf/txt 的
	// HTML/SVG/脚本文件——扩展名与 Content-Type 均由客户端控制，可伪造绕过
	// 校验；此类文件上传到 /storage 直出后会被浏览器 MIME 嗅探执行（存储型 XSS）。
	// 嗅探到的字节拼回 Reader，供 provider 完整写入。
	//
	// 顺序是**先嗅探、后校验**：MIME 白名单的判据应当是文件内容本身，而不是调用方
	// 声明的那句话。只信声明值会把合法文件整批拒掉 —— curl 上传 .webp 时给的是
	// application/octet-stream，实测 449 张图里 178 张 webp 全部失败。扩展名与大小
	// 校验本身不需要文件头，一起挪到嗅探之后能保证两处判定用的是同一份字节。
	sniffed := make([]byte, 512)
	n, _ := io.ReadFull(file.Reader, sniffed)
	var head []byte
	if n > 0 {
		head = sniffed[:n]
		if reason := detectDangerousContent(head); reason != "" {
			return Result{}, fmt.Errorf("上传内容被拒绝（疑似 %s 脚本文件）", reason)
		}
		file.Reader = io.MultiReader(bytes.NewReader(head), file.Reader)
	}
	if err := validateFile(file, head); err != nil {
		return Result{}, err
	}

	// 大小校验的流式兜底：file.Size <= 0（调用方未声明大小或谎报 0）时
	// 不得跳过限制——把 Reader 截断到 maxSize+1 字节，实际大小由 provider
	// 写完后返回的 Size 判定，超限则整体报错（由 provider 清理已落盘文件）。
	sizeUnknown := uploadRules.maxSize > 0 && file.Size <= 0
	if sizeUnknown {
		file.Reader = io.LimitReader(file.Reader, uploadRules.maxSize+1)
	}

	result, err := provider.Upload(ctx, runtime, file, req)
	if err != nil {
		return Result{}, err
	}
	if sizeUnknown && result.Size > uploadRules.maxSize {
		return Result{}, fmt.Errorf("上传文件大小超限: max=%d current=%d", uploadRules.maxSize, result.Size)
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

// validateFile 校验大小 / 扩展名 / MIME 白名单。
//
// head 是调用方嗅探到的文件头（可为空），用于把 MIME 判定锚到**文件内容**上，
// 见 effectiveMIME。
func validateFile(file File, head []byte) error {
	// file.Size > 0：头部声明的大小直接比对（快速拒绝路径）。
	// file.Size <= 0：此处不拒绝，由 uploadWithProvider 的流式 LimitReader 兜底，
	// 按实际读取字节判定是否超限——保证「谎报 Size=0」也绕不过大小校验。
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
		if mime := effectiveMIME(file, head); mime == "" {
			return fmt.Errorf("上传 MIME 类型无法判定: %s", file.ContentType)
		} else if _, ok := uploadRules.allowedMIMETypes[mime]; !ok {
			return fmt.Errorf("上传 MIME 类型不允许: 声明=%s 实际=%s", file.ContentType, mime)
		}
	}

	return nil
}

// DetectMIME 按文件头字节判定 MIME（去参数、小写）；判不出具体类型时返回空串。
//
// 给「需要在 pkg/upload 之外落库或展示 MIME」的调用方用（如 media 模块把附件的
// mime_type 写进 sys_attachment）：客户端声明的 Content-Type 不可信，而 pkg/upload
// 内部的校验已经以内容为准，两处判据必须同源。
func DetectMIME(head []byte) string {
	if len(head) == 0 {
		return ""
	}
	detected := normalizeMIME(http.DetectContentType(head))
	if detected == "application/octet-stream" {
		return ""
	}
	return detected
}

// normalizeMIME 归一化 MIME：小写并去掉 charset 等参数。
func normalizeMIME(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(t, ";"); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	return t
}

// effectiveMIME 决定用于白名单比对的 MIME。
//
// **内容嗅探优先于客户端声明**：扩展名与 Content-Type 都是调用方给的，嗅探结果
// 来自文件本身。只有嗅探不出具体类型（application/octet-stream，例如零字节文件）
// 时才回落到声明值 —— 那条路径不比原来更宽。
func effectiveMIME(file File, head []byte) string {
	if len(head) > 0 {
		detected := normalizeMIME(http.DetectContentType(head))
		if detected != "" && detected != "application/octet-stream" {
			return detected
		}
	}
	return normalizeMIME(file.ContentType)
}

// detectDangerousContent 检测文件头是否属于危险内容（HTML/SVG/XML/脚本）。
//
// 返回非空字符串表示检测到的危险类型（如 "html"、"svg"），空串表示安全。
// 采用两层判定：
//  1. http.DetectContentType 识别常见类型（text/html、image/svg+xml 等）；
//  2. 内容前缀模式匹配（去空白与 UTF-8 BOM 后检测 <?xml/<svg/<!doctype/<html/<script），
//     兜底 Go sniff 算法不识别 SVG 的情况。
func detectDangerousContent(head []byte) string {
	detected := http.DetectContentType(head)
	mediaType := detected
	if i := strings.Index(mediaType, ";"); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml", "text/javascript", "application/javascript", "image/svg+xml":
		return mediaType
	}

	trimmed := bytes.TrimLeft(head, " \t\r\n\xEF\xBB\xBF") // 去空白与 UTF-8 BOM
	lower := bytes.ToLower(trimmed)
	switch {
	case bytes.HasPrefix(lower, []byte("<?xml")),
		bytes.HasPrefix(lower, []byte("<svg")),
		bytes.HasPrefix(lower, []byte("<!doctype")),
		bytes.HasPrefix(lower, []byte("<html")),
		bytes.HasPrefix(lower, []byte("<script")):
		return "html/svg/xml"
	}
	return ""
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
