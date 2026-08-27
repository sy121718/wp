package core

import "strings"

// RenderContext 单次编译的渲染上下文：HTML 缓冲、CSS 收集器与编译期外部服务。
type RenderContext struct {
	HTML *strings.Builder
	CSS  *CSSBuckets
	// Media 媒体解析器（构建期元数据注入）。使用媒体引用的组件（core.image、
	// 后续 core.video 等）依赖它；未注入时相关组件渲染将返回明确错误。
	Media MediaResolver
	// Content CMS 内容解析器（构建期动态绑定注入）。使用字段绑定的组件
	// （core.heading 的 post.title 等）依赖它；未注入时绑定组件渲染返回明确错误。
	Content ContentResolver
}

// 媒体类型常量（与媒体模块资产类型一致）。
const (
	MediaTypeImage    = "image"
	MediaTypeVideo    = "video"
	MediaTypeSVG      = "svg"
	MediaTypeDocument = "document"
)

// PictureSource <picture><source> 条目（现代格式优先声明）。
type PictureSource struct {
	Type   string // 如 image/webp、image/avif
	Srcset string // 完整 srcset 值
}

// MediaMeta 构建期媒体元数据快照：由媒体模块按 assetId 解析产出（规范 docs/02-B §4）。
// 覆盖图片/视频/SVG/文档：图片类携带宽高与响应式变体集合，视频/文档类至少提供稳定 URL。
type MediaMeta struct {
	// Type 媒体类型：image / video / svg / document。
	Type string
	// URL 资源稳定访问地址（图片为选中变体地址；视频/文档为原文件地址）。
	URL string
	// MimeType MIME 类型。
	MimeType string

	// --- 图片类字段（Type 为 image/svg 时有效） ---

	// Width / Height 选中变体宽高。写入 <img> 杜绝 CLS。
	Width  int
	Height int
	// Srcset 同格式多宽度 srcset 值；为空表示单图输出。
	Srcset string
	// Sources 现代格式（WebP/AVIF）的 <source> 列表；非空时输出 <picture>。
	Sources []PictureSource

	// --- 通用 SEO 元数据 ---

	// Alt / Title 全局默认替代文本与标题（组件可局部覆盖）。
	Alt   string
	Title string
	// Caption 全局默认图注（组件可局部覆盖；非空时编译为 <figure>/<figcaption>）。
	Caption string

	// --- 矢量内联载体（Type 为 svg 时有效） ---

	// SrcHTML 内联内容：SVG 源码等原始可内联标记。
	// 由媒体存储提供（内存实现以 variant.URL 承载源码字符串，生产实现由媒体服务填充）。
	// 组件开启 InlineSVG 时以其替代 <img> 输出，fill/stroke 可经 CSS 控制。
	SrcHTML string
}

// MediaResolver 媒体解析契约（媒体级，非图片级）：assetId + 期望规格 → 编译期元数据。
// 实现方为媒体模块（internal/builder/media 提供内存实现，生产实现对接媒体库存储）。
// 图片组件、视频组件、文档引用在构建期统一经此解析。
type MediaResolver interface {
	// ResolveMedia 按资产 ID 与变体规格（original/large/medium/thumbnail）解析媒体元数据。
	// 非图片类资产忽略 variant。
	ResolveMedia(assetID, variant string) (*MediaMeta, error)
}
// ContentResolver CMS 内容解析契约：绑定字段 → 构建期字符串值。
// 规范 docs/02-C1 §2（Dynamic Binding）：发布期数据完全静态填入。
// 实现方由 CMS 模块提供（Phase 0-A2）；core.heading 等绑定组件经此解析。
type ContentResolver interface {
	// ResolveString 按字段路径（如 "post.title"）解析字符串值；不存在返回空串。
	ResolveString(field string) (string, error)
}
