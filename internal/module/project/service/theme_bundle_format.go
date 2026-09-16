package projectservice

// theme_bundle_format.go — 主题包（Theme Bundle）的格式定义与 zip 编解码（审计 VIS-014）。
//
// 包结构（zip，路径一律正斜杠）：
//
//	manifest.json        必需：格式标识、版本号、来源主题、块/页面/媒体清单
//	tokens.json          必需：设计令牌（themes.settings 去掉结构分区与只读元数据）
//	slots.json           可选：槽位预设（header / footer / slots → 包内块 key）
//	blocks/<key>.json    必需（主题引用了块时）：块文档，文档内所有块引用已改写为 key
//	pages/<key>.json     可选：页面文档（含 path），文档内所有块引用已改写为 key
//	preview.png          可选：预览图（本版格式预留：导出侧不产生、导入侧忽略并记提示）
//	media/<key>          可选：内嵌媒体字节（键见 manifest.media[].bundlePath）
//
// 版本号策略：manifest.schemaVersion 是**整数**，不是语义化版本。
// 理由：包格式的兼容问题是「读不读得懂」的二元判定 —— 整数只回答「我支持 1..N」，
// 判别简单、不会把「1.2 我大概能读」这种政策判断塞进解析器；高版本包一律拒绝并明确
// 报出「包内版本 / 当前支持版本」，低版本包交给 upgradeThemeBundle 逐级升级。
// format 标识（go-wp.theme-bundle）负责挡住「随便一个 zip 被当成主题包」。
//
// 包内**不保存任何块/页面 id**：块与页面在包内只以 key（b1/b2...、p1/p2...）标识，
// 文档里的块引用在导出时被改写为 key。于是「导入必须重新分配 id」不是一条要靠自觉遵守的
// 约定，而是包格式本身的性质 —— 包内根本没有原 id 可以沿用。

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	projectenums "go_wp/internal/module/project/enums"
)

// 主题包格式常量。
const (
	// ThemeBundleFormat manifest.format 的期望值（挡住任意 zip 被当作主题包）。
	ThemeBundleFormat = "go-wp.theme-bundle"
	// ThemeBundleSchemaVersion 当前支持的最高包格式版本。
	ThemeBundleSchemaVersion = 1

	themeBundleManifestName = "manifest.json"
	themeBundleTokensName   = "tokens.json"
	themeBundleSlotsName    = "slots.json"
	themeBundleBlocksPrefix = "blocks/"
	themeBundlePagesPrefix  = "pages/"
	themeBundleMediaPrefix  = "media/"
	themeBundlePreviewName  = "preview.png"

	// 未信任输入防线（与 plugin 包安装同一口径）。
	themeBundleMaxZipBytes   = 64 << 20 // 包字节上限
	themeBundleMaxUnpacked   = 128 << 20
	themeBundleMaxEntries    = 600
	themeBundleMaxEntryBytes = 24 << 20
	themeBundleMaxBlocks     = 200
	themeBundleMaxPages      = 200
	themeBundleMaxMediaFiles = 300

	// 媒体缺失原因。
	themeBundleReasonNotFoundInSource = "not_found_in_source"
	themeBundleReasonNotInBundle      = "not_in_bundle"
	themeBundleReasonDeclaredOnly     = "declared_only"
)

// 主题包业务错误哨兵（errors.Is 判型；文案取 projectenums，不硬编码）。
var (
	ErrThemeBundleFileRequired    = errors.New(projectenums.ErrThemeBundleFileRequired)
	ErrThemeBundleFormatUnknown   = errors.New(projectenums.ErrThemeBundleFormatUnknown)
	ErrThemeBundleMissingManifest = errors.New(projectenums.ErrThemeBundleMissingManifest)
	ErrThemeBundleManifestInvalid = errors.New(projectenums.ErrThemeBundleManifestInvalid)
	ErrThemeBundleVersionTooNew   = errors.New(projectenums.ErrThemeBundleVersionTooNew)
	ErrThemeBundleVersionInvalid  = errors.New(projectenums.ErrThemeBundleVersionInvalid)
	ErrThemeBundleUnsafeEntry     = errors.New(projectenums.ErrThemeBundleUnsafeEntry)
	ErrThemeBundleTooLarge        = errors.New(projectenums.ErrThemeBundleTooLarge)
	ErrThemeBundleTokensInvalid   = errors.New(projectenums.ErrThemeBundleTokensInvalid)
	ErrThemeBundleBlockMissing    = errors.New(projectenums.ErrThemeBundleBlockMissing)
	ErrThemeBundleBlockCycle      = errors.New(projectenums.ErrThemeBundleBlockCycle)
	ErrThemeBundleAssetMissing    = errors.New(projectenums.ErrThemeBundleAssetMissing)
	ErrThemeBundlePortUnavailable = errors.New(projectenums.ErrThemeBundlePortUnavailable)
)

// bundleNewline 写包内 JSON 文件时统一补的尾部换行字节。
// 用常量而不是字符字面量：包内容要跨文件系统与编辑器往返，换行写法不该随源码转义走样。
const bundleNewline = byte(0x0a)

// themeBundleZipTime 包内条目的固定时间戳。
// 固定值而不是当前时间：同一份内容重复导出得到同一串字节（确定性，便于比对与缓存校验），
// 时间信息由 manifest.createdAt 单独承载。
var themeBundleZipTime = time.Unix(0, 0).UTC()

// themeBundleManifest 包描述文件。
type themeBundleManifest struct {
	Format        string                  `json:"format"`
	SchemaVersion int                     `json:"schemaVersion"`
	Generator     string                  `json:"generator"`
	CreatedAt     string                  `json:"createdAt"`
	Source        themeBundleSource       `json:"source"`
	Theme         themeBundleThemeMeta    `json:"theme"`
	Blocks        []themeBundleBlockEntry `json:"blocks,omitempty"`
	Pages         []themeBundlePageEntry  `json:"pages,omitempty"`
	Media         []themeBundleMediaEntry `json:"media,omitempty"`
}

// themeBundleSource 包来源（仅溯源，不参与兼容判断）。
type themeBundleSource struct {
	ProjectID string `json:"projectId,omitempty"`
	ThemeID   string `json:"themeId,omitempty"`
	ThemeName string `json:"themeName,omitempty"`
}

// themeBundleThemeMeta 主题元数据 + 槽位预设。
type themeBundleThemeMeta struct {
	Name  string           `json:"name"`
	Slots themeBundleSlots `json:"slots"`
}

// themeBundleSlots 槽位预设（值为包内块 key）。
//
// 两个通道与 themes.settings 的既有形状一一对应（headerBlockId / footerBlockId / slots）：
// 包内不改写存储语义，导入时再按映射落回同样的键。
type themeBundleSlots struct {
	Header string            `json:"header,omitempty"`
	Footer string            `json:"footer,omitempty"`
	Slots  map[string]string `json:"slots,omitempty"`
}

// isEmpty 是否没有任何槽位预设。
func (s themeBundleSlots) isEmpty() bool {
	return strings.TrimSpace(s.Header) == "" && strings.TrimSpace(s.Footer) == "" && len(s.Slots) == 0
}

// themeBundleBlockEntry 块清单条目。
type themeBundleBlockEntry struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Category  string `json:"category,omitempty"`
	ReuseMode string `json:"reuseMode,omitempty"`
	// Refs 该块文档引用到的其它块 key（导入拓扑排序与完整性校验用）。
	Refs []string `json:"refs,omitempty"`
}

// themeBundlePageEntry 页面清单条目。
type themeBundlePageEntry struct {
	Key  string `json:"key"`
	Kind string `json:"kind,omitempty"`
	Path string `json:"path"`
}

// themeBundleMediaEntry 媒体清单条目。
type themeBundleMediaEntry struct {
	Key          string   `json:"key"`
	URL          string   `json:"url"`
	Path         string   `json:"path"`
	SHA256       string   `json:"sha256,omitempty"`
	Size         int64    `json:"size,omitempty"`
	Embedded     bool     `json:"embedded"`
	BundlePath   string   `json:"bundlePath,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	ReferencedBy []string `json:"referencedBy,omitempty"`
}

// themeBundleBlockFile 包内 blocks/<key>.json。
type themeBundleBlockFile struct {
	Key       string          `json:"key"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Category  string          `json:"category,omitempty"`
	ReuseMode string          `json:"reuseMode,omitempty"`
	Document  json.RawMessage `json:"document"`
}

// themeBundlePageFile 包内 pages/<key>.json。
type themeBundlePageFile struct {
	Key      string          `json:"key"`
	Kind     string          `json:"kind,omitempty"`
	Path     string          `json:"path"`
	Document json.RawMessage `json:"document"`
}

// checkThemeBundleVersion 校验包格式版本。
//
// 高版本一律拒绝：包里可能有本版解析器读不懂的字段，静默降级导入的结果是
// 「看起来成功、实际少了一部分主题」—— 比拒绝更难排查。
func checkThemeBundleVersion(v int) error {
	switch {
	case v <= 0:
		return ErrThemeBundleVersionInvalid
	case v > ThemeBundleSchemaVersion:
		return ErrThemeBundleVersionTooNew
	}
	return nil
}

// upgradeThemeBundle 把低版本包升到当前版本。
// 当前只有 v1（首个版本），留成显式分支是为了下一个版本必须在**这里**写下升级规则，
// 而不是散落到解析各处。
func upgradeThemeBundle(v int) error {
	switch v {
	case ThemeBundleSchemaVersion:
		return nil
	default:
		return ErrThemeBundleVersionInvalid
	}
}

// parseThemeBundleManifest 解析并校验 manifest。
func parseThemeBundleManifest(raw []byte) (m *themeBundleManifest, err error) {
	m = &themeBundleManifest{}
	if err = json.Unmarshal(raw, m); err != nil {
		return nil, ErrThemeBundleManifestInvalid
	}
	if strings.TrimSpace(m.Format) != ThemeBundleFormat {
		return nil, ErrThemeBundleFormatUnknown
	}
	if verr := checkThemeBundleVersion(m.SchemaVersion); verr != nil {
		return nil, verr
	}
	if verr := upgradeThemeBundle(m.SchemaVersion); verr != nil {
		return nil, verr
	}
	if strings.TrimSpace(m.Theme.Name) == "" {
		return nil, ErrThemeBundleManifestInvalid
	}
	if len(m.Blocks) > themeBundleMaxBlocks || len(m.Pages) > themeBundleMaxPages || len(m.Media) > themeBundleMaxMediaFiles {
		return nil, ErrThemeBundleTooLarge
	}
	for _, b := range m.Blocks {
		if strings.TrimSpace(b.Key) == "" {
			return nil, ErrThemeBundleManifestInvalid
		}
	}
	for _, p := range m.Pages {
		if strings.TrimSpace(p.Key) == "" || strings.TrimSpace(p.Path) == "" {
			return nil, ErrThemeBundleManifestInvalid
		}
	}
	return m, nil
}

// marshalThemeBundleManifest 序列化 manifest（缩进输出，便于人工查看包内容）。
func marshalThemeBundleManifest(m *themeBundleManifest) ([]byte, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, bundleNewline), nil
}

// safeBundleEntryName 校验并归一化包内条目名。
//
// 拒绝：空名、绝对路径、盘符、反斜杠、任何上溯段（zip slip）、目录条目。
// 归一化后必须与原名一致（多出来的斜杠/点段也一并拒绝，避免同名不同写法绕过去重）。
func safeBundleEntryName(name string) (string, bool) {
	if name == "" || strings.IndexByte(name, 0x5c) >= 0 || strings.HasPrefix(name, "/") {
		return "", false
	}
	if strings.IndexByte(name, 0x3a) >= 0 || strings.HasSuffix(name, "/") {
		return "", false
	}
	clean := path.Clean(name)
	if clean != name {
		return "", false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	if !themeBundleEntryAllowed(clean) {
		return "", false
	}
	return clean, true
}

// themeBundleEntryAllowed 包内条目名白名单：只有清单、令牌、槽位、块/页面文档、预览图与媒体。
func themeBundleEntryAllowed(name string) bool {
	switch name {
	case themeBundleManifestName, themeBundleTokensName, themeBundleSlotsName, themeBundlePreviewName:
		return true
	}
	switch {
	case strings.HasPrefix(name, themeBundleBlocksPrefix),
		strings.HasPrefix(name, themeBundlePagesPrefix):
		return strings.HasSuffix(name, ".json")
	case strings.HasPrefix(name, themeBundleMediaPrefix):
		return true
	}
	return false
}

// readThemeBundleZip 解包到内存（全部校验通过才返回），带条目数/尺寸/路径三重防线。
func readThemeBundleZip(data []byte) (files map[string][]byte, err error) {
	if len(data) == 0 {
		return nil, ErrThemeBundleFileRequired
	}
	if len(data) > themeBundleMaxZipBytes {
		return nil, ErrThemeBundleTooLarge
	}
	zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if zerr != nil {
		return nil, ErrThemeBundleFormatUnknown
	}
	if len(zr.File) > themeBundleMaxEntries {
		return nil, ErrThemeBundleTooLarge
	}
	files = make(map[string][]byte, len(zr.File))
	var total int64
	for _, f := range zr.File {
		name, ok := safeBundleEntryName(f.Name)
		if !ok {
			return nil, ErrThemeBundleUnsafeEntry
		}
		if f.UncompressedSize64 > themeBundleMaxEntryBytes {
			return nil, ErrThemeBundleTooLarge
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return nil, ErrThemeBundleFormatUnknown
		}
		content, rerr := io.ReadAll(io.LimitReader(rc, themeBundleMaxEntryBytes+1))
		_ = rc.Close()
		if rerr != nil {
			return nil, ErrThemeBundleFormatUnknown
		}
		if int64(len(content)) > themeBundleMaxEntryBytes {
			return nil, ErrThemeBundleTooLarge
		}
		total += int64(len(content))
		if total > themeBundleMaxUnpacked {
			return nil, ErrThemeBundleTooLarge
		}
		if _, dup := files[name]; dup {
			return nil, ErrThemeBundleUnsafeEntry
		}
		files[name] = content
	}
	if _, ok := files[themeBundleManifestName]; !ok {
		return nil, ErrThemeBundleMissingManifest
	}
	return files, nil
}

// themeBundleZipWriter 打包助手：固定时间戳、固定权限位（确定性字节）。
type themeBundleZipWriter struct {
	zw *zip.Writer
}

// newThemeBundleZipWriter 创建打包器（返回写入器与承载字节的缓冲）。
func newThemeBundleZipWriter() (*themeBundleZipWriter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &themeBundleZipWriter{zw: zip.NewWriter(buf)}, buf
}

// add 写入一个条目。
func (w *themeBundleZipWriter) add(name string, data []byte) error {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: themeBundleZipTime}
	h.SetMode(0o644)
	f, err := w.zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// close 结束打包。
func (w *themeBundleZipWriter) close() error { return w.zw.Close() }

// sortedKeys 返回有序键（确定性遍历：包里条目顺序、清单顺序都由它决定）。
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cleanBundleName 把主题名转成安全的包文件名。
func cleanBundleName(name string) string {
	trimmed := strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		out = "theme"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out + ".skintheme.zip"
}

// themeBundleGenerator 包生成者标识（写在 manifest 里，便于排查「这个包是谁导出的」）。
const themeBundleGenerator = "go_wp/project.theme-bundle"

// themeBundleManifestCreatedAt 导出时间戳（RFC3339，秒精度）。
func themeBundleManifestCreatedAt(now time.Time) string {
	return now.UTC().Format("2006-01-02T15:04:05Z")
}

// bundleKey 生成包内 key（b1/b2...、p1/p2...、m1/m2...）。
func bundleKey(prefix string, idx int) string {
	return fmt.Sprintf("%s%d", prefix, idx)
}
