// Package pagecontract 定义 page 模块对外能力。
package pagecontract

import (
	"context"
	"encoding/json"
	"errors"

	pagedto "go_wp/internal/module/page/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import page/dto。
type (
	CreateReq     = pagedto.CreateReq
	SaveDraftReq  = pagedto.SaveDraftReq
	DetailReq     = pagedto.DetailReq
	RevisionReq   = pagedto.RevisionReq
	RevisionResp  = pagedto.RevisionResp
	DeleteReq     = pagedto.DeleteReq
	PageResp      = pagedto.PageResp
	PageDraftResp = pagedto.PageDraftResp
	BuildReq      = pagedto.BuildReq
	PublishReq    = pagedto.PublishReq
	RollbackReq   = pagedto.RollbackReq
	UpdateURLReq  = pagedto.UpdateURLReq
	PublishResp   = pagedto.PublishResp
)

// 预览编译错误哨兵：dashboard 预览复用本契约的编译能力时，
// 经 errors.Is 精确分类 HTTP 状态码（解析失败 400 / 编译失败 422 / 其余 500），
// 文案由调用方（dashboard enums）自行下发，此处仅作错误类型标识。
var (
	// ErrPreviewInvalidDocument 预览文档解析失败（JSON 非法或空文档）。
	ErrPreviewInvalidDocument = errors.New("预览文档解析失败")
	// ErrPreviewCompileFailed 预览编译失败。
	ErrPreviewCompileFailed = errors.New("预览编译失败")
)

// PageService 手工 Page 草稿、修订与发布管理能力。
type PageService interface {
	Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error)
	// List 列出页面摘要（themeID 为空时列全部；非空时只列挂在该主题下的页面）。
	List(ctx context.Context, themeID string) (res []pagedto.PageResp, err error)
	Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error)
	// ListDrafts 列出全部未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描：
	// 跨页面复用提示与全站完成度分母需要 (source_hash, context) 的全站视图）。
	ListDrafts(ctx context.Context) (res []pagedto.PageDraftResp, err error)
	SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error)
	ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error)

	// CompilePreview 基于未落盘文档 JSON 编译完整 HTML（预览专用：不落盘、不影响产物）。
	// 复用正式构建同源编译管线；错误经 errors.Is 分类：
	// ErrPreviewInvalidDocument（解析失败）/ ErrPreviewCompileFailed（编译失败）/ 其余为内部错误。
	// projectID 为页面所属站点工程（驱动导航等站点级资源解析，与正式构建一致）；
	// currentPath 为页面逻辑访问路径（导航当前项高亮，块预览传空）；
	// lang 为预览目标语言（空 = 站点默认语言，多语言 P2）。
	CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string) (html []byte, err error)
	// Build 基于当前草稿构建并暂存产物（不激活线上）。
	Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error)
	// Publish 激活暂存产物。
	Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error)
	// Rollback 秒级回滚到历史产物。
	Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error)
	// UpdateURL 修改访问路径，旧路径按策略 301 或取消激活。
	UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error)
	// Delete 软删页面（deleted_at 置时间，审计留痕）并释放其全部路径占用
	// （reserved/active/redirect），同路径可被新页面重新占用。
	Delete(ctx context.Context, req *pagedto.DeleteReq) (err error)
	// RefreshThemeForTheme 把主题设置批量合入挂在该主题下全部页面（主题设置保存后调用）。
	RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error
	// RefreshStructureForTheme 把主题的页眉/页脚块绑定批量合入挂在该主题下
	// 全部页面的 settings.structure（主题换绑全局块后调用）。
	RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error
	// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用）。
	MarkStaleForTheme(ctx context.Context, themeID string) error
	// MarkStaleForBlock 把文档中经 core.globalref 引用或 settings.structure 页眉/页脚
	// 自选绑定该块的页面标记为待重建（块内容变更后调用，与 MarkStaleForTheme 互补）。
	MarkStaleForBlock(ctx context.Context, blockID string) error
	// MarkStaleForI18n 把全部页面标记为待重建（界面文案词条变更后调用，
	// 与 Manifest 的 i18n 依赖条目配套，docs/06-D §10.4）。
	MarkStaleForI18n(ctx context.Context) error
	// CountBlockReference 统计引用该块的未删除页面数（globalref / structure 自选绑定），
	// 供 block 模块删除或切换 global→template 前的引用拦截（docs/02-D §9）。
	CountBlockReference(ctx context.Context, blockID string) (int64, error)
	// AttachThemeToUnassigned 把工程内未挂主题的页面挂到指定主题（工程首个主题创建后回填历史页面）。
	AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error
	// ReattachProjectPagesToTheme 把工程内全部页面（含已挂其他主题的）转挂到指定主题，
	// 用于切换激活主题后的「整站换皮」：使后续 RefreshThemeForTheme/RefreshStructureForTheme/
	// MarkStaleForTheme 以该主题为键命中全部页面。
	ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) error
}
