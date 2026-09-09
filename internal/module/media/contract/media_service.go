// Package mediacontract 定义媒体模块对外暴露的业务契约接口。
package mediacontract

import (
	"context"
	"mime/multipart"

	mediadto "go_wp/internal/module/media/dto"
)

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
	SyncReferences(ctx context.Context, req *mediadto.SyncRefsReq) (int, error)
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
	// GenerateVariants 同步生成/重新生成指定附件的全部图片变体（thumb/medium/webp），
	// 供「重新生成」按钮与存量回填复用；返回生成后的变体状态列表。
	GenerateVariants(ctx context.Context, attachmentID uint64) ([]mediadto.VariantResp, error)
	// ProbeImageVariants 按公开 URL（/storage/...）探测已就绪的图片变体宽度列表，
	// 供构建期响应式图片（srcset）使用；非媒体库 URL 或变体未就绪返回 nil。
	ProbeImageVariants(ctx context.Context, url string) []int
	// BuildDownloadPlan 构建单个附件的资源包（zip）打包计划。
	BuildDownloadPlan(ctx context.Context, attachmentID uint64) (*mediadto.DownloadPlan, error)
	// BuildBatchDownloadPlan 构建多个附件的资源包（zip）批量打包计划。
	BuildBatchDownloadPlan(ctx context.Context, ids []uint64) (*mediadto.DownloadPlan, error)
}
