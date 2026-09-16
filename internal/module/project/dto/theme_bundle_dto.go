package projectdto

// theme_bundle_dto.go — 主题包（Theme Bundle）导入导出的请求与响应形状（审计 VIS-014）。
//
// 主题包是**离线可交付**的站点资产：第三方把「设计令牌 + 页眉页脚块 + 若干页面 + 槽位预设」
// 打包成 zip 交付，目标环境导入后得到一套视觉一致的新主题（块与页面一律重新分配 id）。

// 媒体依赖策略（导出侧）。
const (
	// ThemeBundleMediaEmbed 能读到字节的媒体随包内嵌（读不到的作为缺失声明写进清单）。
	ThemeBundleMediaEmbed = "embed"
	// ThemeBundleMediaDeclare 只声明不内嵌（包体积小，导入侧按声明逐条核对目标环境）。
	ThemeBundleMediaDeclare = "declare"
)

// 导入报告里的媒体处置动作。
const (
	// ThemeBundleMediaActionWritten 内嵌媒体已落盘（目标原本没有这个文件）。
	ThemeBundleMediaActionWritten = "written"
	// ThemeBundleMediaActionSkipped 目标已存在同内容文件（按 sha256 判定），未重复写入。
	ThemeBundleMediaActionSkipped = "skipped_identical"
	// ThemeBundleMediaActionConflict 目标已存在同名但内容不同的文件 —— 不覆盖，明确上报。
	ThemeBundleMediaActionConflict = "conflict_existing"
	// ThemeBundleMediaActionAvailable 包内未内嵌，但目标环境已有该文件（引用可用）。
	ThemeBundleMediaActionAvailable = "available_in_target"
)

// ThemeBundleExportReq 导出主题包请求。
//
// 参数一律走 query（GET），与项目「只用 GET/POST、不用 RESTful 路径参数」的约定一致。
type ThemeBundleExportReq struct {
	ThemeID string `form:"themeId" json:"themeId" binding:"required"`
	// WithPages 是否带上主题下的页面文档（页面属站点内容不是主题本身，默认不带）。
	WithPages bool `form:"withPages" json:"withPages"`
	// Media 媒体依赖策略：embed（默认，能读到的内嵌）| declare（只声明）。
	Media string `form:"media" json:"media"`
}

// ThemeBundleExportResp 导出结果：包字节与内容统计。
type ThemeBundleExportResp struct {
	FileName string `json:"fileName"`
	Bytes    []byte `json:"-"`
	ThemeID  string `json:"themeId"`
	// BlockCount / PageCount 包内块与页面数量。
	BlockCount int `json:"blockCount"`
	PageCount  int `json:"pageCount"`
	// MediaEmbedded 随包内嵌的媒体数；MediaDeclared 只声明未内嵌的媒体数。
	MediaEmbedded int `json:"mediaEmbedded"`
	MediaDeclared int `json:"mediaDeclared"`
	// Warnings 导出期的非阻断问题（悬空块引用、读不到的媒体等）。
	Warnings []string `json:"warnings,omitempty"`
}

// ThemeBundleImportReq 导入主题包请求。
type ThemeBundleImportReq struct {
	ProjectID string `json:"projectId"`
	// Name 导入后的主题名；空则用包内名称（重名时自动加后缀，并在报告中说明）。
	Name string `json:"name"`
	// CreatePages 是否创建包内页面（默认只导主题与块）。
	CreatePages bool `json:"createPages"`
	// Activate 导入后是否立即激活该主题。
	Activate bool `json:"activate"`
	// FileName 包文件名（仅用于日志与报告）。
	FileName string `json:"fileName"`
	// Zip 包字节（HTTP 层从 multipart 读取）。
	Zip []byte `json:"-"`
}

// ThemeBundleImportResp 导入报告。
type ThemeBundleImportResp struct {
	ThemeID   string `json:"themeId"`
	ThemeName string `json:"themeName"`
	// RequestedName 包内（或入参）请求的名称；NameAdjusted 为真表示因重名加了后缀。
	RequestedName string `json:"requestedName"`
	NameAdjusted  bool   `json:"nameAdjusted"`
	Activated     bool   `json:"activated"`
	// SchemaVersion 包内声明的格式版本（已通过兼容性校验）。
	SchemaVersion int `json:"schemaVersion"`
	// Blocks 块 key → 新分配 id 的映射（原 id 一律不沿用）。
	Blocks []ThemeBundleImportedBlock `json:"blocks"`
	// Pages 页面 key → 新分配 id。
	Pages []ThemeBundleImportedPage `json:"pages"`
	// Media 媒体依赖处置结果（内嵌落盘 / 目标已有 / 冲突 / 缺失清单）。
	Media ThemeBundleMediaReport `json:"media"`
	// SkippedPages 未创建的页面（路径在目标工程已被占用等），逐条给出原因。
	SkippedPages []ThemeBundlePageSkip `json:"skippedPages,omitempty"`
	// Warnings 其它非阻断提示。
	Warnings []string `json:"warnings,omitempty"`
}

// ThemeBundleImportedBlock 块导入结果（key → 新 id）。
type ThemeBundleImportedBlock struct {
	Key       string `json:"key"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	ReuseMode string `json:"reuseMode,omitempty"`
}

// ThemeBundleImportedPage 页面导入结果（key → 新 id）。
type ThemeBundleImportedPage struct {
	Key  string `json:"key"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

// ThemeBundlePageSkip 未创建页面的原因。
type ThemeBundlePageSkip struct {
	Key    string `json:"key"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ThemeBundleMediaReport 媒体依赖处置报告。
//
// 三份清单互斥：Media.Resolved 是「引用可用」的（内嵌落盘 / 目标已有），
// Media.Conflicts 是「目标有同名但内容不同」（未覆盖，链接指向的是目标既有文件），
// Media.Missing 是「引用不到」的 —— 后者就是「缺失清单」，导入侧绝不静默留死链。
type ThemeBundleMediaReport struct {
	// Declared 包内声明的媒体引用总数。
	Declared int `json:"declared"`
	// Embedded 随包内嵌的媒体数。
	Embedded int `json:"embedded"`
	// Resolved 引用已经可用的媒体条目。
	Resolved []ThemeBundleMediaItem `json:"resolved"`
	// Conflicts 目标环境已有同名但内容不同的文件（未覆盖）。
	Conflicts []ThemeBundleMediaItem `json:"conflicts,omitempty"`
	// Missing 缺失清单：这些 URL 在目标环境既未内嵌也不存在，产物引用会指向不存在的文件。
	Missing []ThemeBundleMissingMedia `json:"missing,omitempty"`
}

// ThemeBundleMediaItem 单条媒体的处置结果。
type ThemeBundleMediaItem struct {
	Key    string `json:"key"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Action string `json:"action"`
	// ReferencedBy 引用来源（block:<key> / page:<key>），便于定位要修哪一块。
	ReferencedBy []string `json:"referencedBy,omitempty"`
}

// ThemeBundleMissingMedia 缺失媒体条目（明确清单，不是静默死链）。
type ThemeBundleMissingMedia struct {
	Key  string `json:"key"`
	URL  string `json:"url"`
	Path string `json:"path"`
	// Reason 缺失原因：not_found_in_source（导出侧读不到字节）/ not_in_bundle（包内未内嵌且目标没有）。
	Reason string `json:"reason"`
	// WillDeadLink 恒为真：这条 URL 在导入后的站点里指向不存在的文件。
	WillDeadLink bool `json:"willDeadLink"`
	// ReferencedBy 引用来源清单（块 key / 页面 key）。
	ReferencedBy []string `json:"referencedBy,omitempty"`
}
