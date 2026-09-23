package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	uploadprovider "go_wp/pkg/upload/provider"

	"github.com/spf13/viper"
)

// localProviderDefaultDir 本地存储默认根目录，与 provider 包 defaultLocalDir 同值
// （provider 包未导出该常量，这里同步维护，LocalDir 未初始化兜底用）。
const localProviderDefaultDir = "public/storage"

// defaultMediaBaseURL 媒体资源对外前缀的缺省值（未配置 upload.base_url 时）。
const defaultMediaBaseURL = "/storage"

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
	// mediaBaseURL 媒体资源的对外前缀（upload.base_url 配置，形如
	// "http://host:8080/storage"；未配置时 "/storage"）。
	//
	// 存在的理由：**URL 是派生值，不该在入库那一刻把域名冻进 sys_attachment.url**。
	// 冻结版有三处各拼各的（provider 上传结果、附件读回、变体读回），配了 base_url
	// 也只有第一处生效；换域名时更要靠迁移回填，且回填不到已经复制进产物与 Page
	// Document 的那些副本。改成读取时派生后，配置一改、全站同时生效。
	mediaBaseURL = defaultMediaBaseURL
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
	mediaBaseURL = mediaBaseURLFromConfig(v)
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

// mediaBaseURLFromConfig 解析 upload.base_url 得到媒体对外前缀。
//
// 配置值是**站点根**（如 "http://host:8080"），本函数补上 "/storage" 路径段 ——
// 与 provider 侧 local.go 的拼接口径逐字一致（那边也是 siteURL + "/storage"）。
// 配了 "/" 或带尾斜杠都归一；配成已含 "/storage" 的值不再重复追加。
func mediaBaseURLFromConfig(v *viper.Viper) string {
	if v == nil {
		return defaultMediaBaseURL
	}
	raw := strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(v.GetString("upload.base_url")), "\\", "/"), "/")
	if raw == "" {
		return defaultMediaBaseURL
	}
	if strings.HasSuffix(raw, "/storage") {
		return raw
	}
	return raw + "/storage"
}

// BaseURL 媒体资源的对外前缀（不含末尾斜杠）。
//
// 未配置 upload.base_url 时是 "/storage"（相对路径），配置站点根后是完整地址
// （"http://host:8080/storage"）。需要给用户「完整链接」的地方一律经它拼接，
// 不要自己写 "/storage/..." —— 写死的那一处不会跟着配置走。
func BaseURL() string {
	stateMu.RLock()
	base := mediaBaseURL
	stateMu.RUnlock()
	if strings.TrimSpace(base) == "" {
		return defaultMediaBaseURL
	}
	return strings.TrimRight(base, "/")
}

// StorageKey 从「存储键 / 相对 URL / 绝对 URL」中取出存储键（相对 storage 根的那一段）。
//
// 三种历史写法都要认，否则存量行会漂：
//
//	"482.jpg"                        → "482.jpg"
//	"/storage/482.jpg"               → "482.jpg"（入库缺省形态）
//	"http://old-host/storage/482.jpg" → "482.jpg"（配过 base_url 的旧数据）
//
// 取不到键（外链 CDN / data: URI / 空串）返回空串 —— 调用方据此区分
// 「这是我们自己存的媒体」与「这是作者贴的外部地址」，不要替前者静默改写。
func StorageKey(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if idx := strings.Index(u, "/storage/"); idx >= 0 {
		return strings.TrimLeft(u[idx+len("/storage/"):], "/")
	}
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") ||
		strings.HasPrefix(u, "//") {
		return ""
	}
	key := strings.TrimLeft(u, "/")
	// "storage/482.jpg" 这种缺前导斜杠的写法同样要剥段 —— 不剥会拼成
	// "/storage/storage/482.jpg"（实测踩过）。
	if rest, ok := strings.CutPrefix(key, "storage/"); ok {
		return rest
	}
	return key
}

// StorageURL 把存储标识拼成对外可用的媒体 URL。
//
// 能取出存储键的一律**按当前配置重新拼**（不保留原串里的主机名）：
// 换域名后存量附件要跟着新配置走，而不是永远指着旧主机。
// 取不到键但本身是绝对地址（CDN 外链 / data: URI）时原样返回 ——
// 补前缀会拼出 "http://host/storage/https://cdn/x.jpg" 这种废串。
//
// 传空串返回空串（调用方据此判「没配图」，不是返回 "/storage/" 这种半截地址）。
func StorageURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if key := StorageKey(u); key != "" {
		return BaseURL() + "/" + key
	}
	// 取不到键：data: / blob: / //host 这类，原样返回。
	return u
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
