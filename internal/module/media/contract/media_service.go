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
	// Delete 删除附件（同时删除物理文件）。
	Delete(ctx context.Context, req *mediadto.DeleteReq) error
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
}
