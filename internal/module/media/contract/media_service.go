// Package mediacontract 定义媒体模块对外暴露的业务契约接口。
package mediacontract

import (
	"context"
	"mime/multipart"

	mediadto "go_wp/internal/module/media/dto"
)

// VariantRef 已就绪的图片变体引用（构建期 srcset 消费）—— mediadto 的重导出。
//
// 为什么不另造一组「契约自有形状」：它要传给 builder / pipeline / page / presentation
// 四个消费方，两处逐字段等价的定义等于把「一处改、调用方编译错」换成「一处改、
// 另一处静默分叉」。真源在 dto，这里只做跨模块可见性的显式声明。
type VariantRef = mediadto.VariantRef

// SyncRefsInput 引用同步的入参 —— 契约自有形状，不是 dto 的别名。
//
// 四个字段就是这件事的全部语义：谁引用（RefKind + RefID）、引用方标题（RefTitle）、
// 引用了哪些媒体 URL（URLs）。dto 那侧带 JSON 名与绑定标签，形状随 HTTP 接口变；
// 契约形状只随语义变。
//
// URLs 为空即「该引用方不再引用任何媒体」—— 页面删除走的就是这条（清空引用再软删），
// 不需要额外开关字段。
type SyncRefsInput struct {
	// RefKind 引用方类型（如 page）。
	RefKind string
	// RefID 引用方标识。
	RefID string
	// RefTitle 引用方标题（展示用，可为空）。
	RefTitle string
	// URLs 该引用方产物中出现的媒体 URL 全集；空集表示解除全部引用。
	URLs []string
}

// MediaService 定义媒体模块对外暴露的业务能力。
type MediaService interface {
	// Upload 上传文件并记录附件元数据。
	Upload(ctx context.Context, file *multipart.FileHeader, categoryID *uint64) (*mediadto.AttachmentResp, error)
	// List 分页查询附件列表。
	List(ctx context.Context, req *mediadto.ListReq) (*mediadto.ListResp, error)
	// Detail 查询单个附件详情。
	Detail(ctx context.Context, req *mediadto.DetailReq) (*mediadto.AttachmentResp, error)
	// Delete 删除附件（软删除；被引用时拒绝，见 References）。
	Delete(ctx context.Context, req *mediadto.DeleteReq) error
	// Replace 换图：内容替换、URL 不变（/storage/<id>.<ext>）、generation+1；
	// 扩展名必须与现路径一致，内容 md5 未变时直接返回现状。
	Replace(ctx context.Context, id uint64, file *multipart.FileHeader) (*mediadto.AttachmentResp, error)
	// References 查询附件的引用来源（构建期写入 extra_info.refs 的缓存）。
	References(ctx context.Context, id uint64) ([]mediadto.AttachmentRefResp, error)
	// SyncReferences 全量同步某引用方对媒体库的引用（refs 写入侧，构建期调用，幂等）。
	SyncReferences(ctx context.Context, req *SyncRefsInput) (int, error)
	// SyncReferencesFromHTML 从产物 HTML 收集媒体引用并全量同步（构建期便捷入口）。
	SyncReferencesFromHTML(ctx context.Context, refKind, refID, refTitle, html string) (int, error)
	// CreateCategory 新建分类（无限级，同父级下重名拒绝）。
	CreateCategory(ctx context.Context, req *mediadto.CategoryCreateReq) (*mediadto.CategoryTreeNode, error)
	// UpdateCategory 更新分类（改名/移动父级/排序，移动防环）。
	UpdateCategory(ctx context.Context, req *mediadto.CategoryUpdateReq) error
	// DeleteCategory 删除分类（有子级拒绝；附件移入未分类）。
	DeleteCategory(ctx context.Context, req *mediadto.CategoryDeleteReq) error
	// UpdateAttachment 更新附件元数据（文件名/分类/alt/标题/描述）。
	UpdateAttachment(ctx context.Context, req *mediadto.AttachmentUpdateReq) error
	// CategoryTree 获取文件分类树。
	CategoryTree(ctx context.Context) ([]mediadto.CategoryTreeNode, error)
	// GenerateVariants 同步生成/重新生成指定附件的全部图片变体（thumb/medium/full），
	// 供「重新生成」按钮与存量回填复用；返回生成后的变体状态列表。
	GenerateVariants(ctx context.Context, attachmentID uint64) ([]mediadto.VariantResp, error)
	// ProbeImageVariants 按公开 URL（/storage/...）探测已就绪的图片变体，
	// 供构建期响应式图片（srcset）使用；非媒体库 URL 或变体未就绪返回 nil。
	//
	// 返回**完整 URL + 宽度**而不是只返回宽度：变体文件名带内容指纹
	// （<stem>_<type>-<generation>-<hash8>.jpg），而 generation 与 hash 只有本模块知道。
	// 调用方按宽度自行拼文件名 = 第二份命名真源，改一处就是 srcset 静默指向不存在的文件。
	ProbeImageVariants(ctx context.Context, url string) []mediadto.VariantRef
	// BuildDownloadPlan 构建单个附件的资源包（zip）打包计划。
	//
	// langs 是可选的**产物语言**（zip 内 README.txt 用）：不传时按默认语言生成。
	// 它必须显式传进来而不是从 ctx 取 —— service 层拿不到请求语言，而 README
	// 是要落进用户下载的文件里的文案，写死一种语言等于把中文焊进导出产物。
	BuildDownloadPlan(ctx context.Context, attachmentID uint64, langs ...string) (*mediadto.DownloadPlan, error)
	// BuildBatchDownloadPlan 构建多个附件的资源包（zip）批量打包计划（langs 同上）。
	BuildBatchDownloadPlan(ctx context.Context, ids []uint64, langs ...string) (*mediadto.DownloadPlan, error)
}

// === 索要的端口 ===

// 变体文件名带内容指纹（<stem>_<type>-<generation>-<hash8>.jpg，见 ProbeImageVariants），
// 于是「同一 URL 的字节不可变」成立，/storage 才敢给它 immutable 长缓存。
// 代价是把「内容变了要通知引用方」变成 media 的义务：
//
//	换图 → generation+1 → 一组**新的**变体文件名 → 已发布产物里的 srcset 仍指向旧名，
//	而旧文件按设计保留（见 media/service/media_replace.go），旧 URL 继续返回旧字节。
//	访客端 srcset 按 sizes 选中的**多数是变体而不是 src**，所以「换图后新图不可见」
//	不是缓存 bug，是设计缺口 —— 唯一出口是引用方重建产物、新产物引用新名。
//
// 引用集（谁引用了这张图）就在本模块的 sys_attachment.extra_info.refs 里，
// 而「页面 / 实例怎么算待重建」在对方模块的表里 —— 跨模块表访问是禁止的
//（AGENTS.md §表隔离），所以这里只声明索要的端口，由引用方模块实现、装配层注入。

// 引用方类型常量：与 extra_info.refs 的 kind 取值一一对应 —— 写入侧是 media 自己
// （SyncReferences 的 RefKind），读取侧（换图失效通知）也在这里，两边不会各写一份字符串。
// 新增一种引用方时**必须同时**给出实现并在装配期注入，否则 SetStaleMarkers 当场失败。
const (
	// RefKindPage 手工页面（page 模块的发布产物）。
	RefKindPage = "page"
	// RefKindPresentation 自动发布实例（presentation 模块的发布产物）。
	RefKindPresentation = "presentation"
)

// MediaRef 一条「引用方标识」（引用集里的一项）。
//
// 契约自有形状，不是任何模块 dto 的别名：只有 kind 与 id 两个语义字段 ——
// 标题之类的展示加成是引用方自己的事，media 不该认识。
type MediaRef struct {
	// Kind 引用方类型（RefKind* 之一）。
	Kind string
	// ID 引用方标识（页面 id / 实例 id）。
	ID string
}

// StaleMarker 换图失效通知端口（引用方模块实现，装配层注入）。
//
// 实现准则（与其它装配期端口同）：
//   - 只认领 RefKinds() 声明的类型，入参里不会有别的 kind；
//   - 返回**真正命中**的引用方 id（RETURNING 回读），不是入参回显 ——
//     换图的影响面日志读这个集合，回显会让「说标了 8 个、其实只有 3 个存在」查不出来；
//   - 失败即失败：**禁止**在实现里吞掉错误后返回空集合 —— 那会让「通知没送达」与
//     「没有引用方」在下游完全同形，而这两件事的处置动作相反。
type StaleMarker interface {
	// RefKinds 本实现认领的引用方类型（取 RefKind* 常量）。
	RefKinds() []string
	// MarkStaleByMediaRefs 把 refs 描述的引用方标记为待重建，返回真正命中的引用方 ID。
	MarkStaleByMediaRefs(ctx context.Context, refs []MediaRef) (marked []string, err error)
}
