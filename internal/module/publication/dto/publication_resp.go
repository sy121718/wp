package pubdto

import (
	"go_wp/pkg/utils"
)

// RouteResp 路由占用投影。
type RouteResp struct {
	ProjectID string  `json:"projectId"`
	Path      string  `json:"path"`
	PageID    *string `json:"pageId,omitempty"`
	// PresentationID 展示实例归属（页面占用的行此字段为空）。
	PresentationID *string        `json:"presentationId,omitempty"`
	RouteKind      string         `json:"routeKind"`
	ArtifactID     *string        `json:"artifactId,omitempty"`
	UpdatedAt      utils.JSONTime `json:"updatedAt"`
}

// 访问面切换回执的三种动作名。
//
// 恢复流程按它分派补齐例程：三种动作「已切换访问面、数据库没跟上」时要补的
// 数据库步骤各不相同（发布补活跃指针、改 URL 补路径迁移与路由、回滚补活跃指针
// 与旧路径下线），用一个动作名会让恢复猜错方向。
//
// 与此并存的是路由回执（Activate 的 "activate" / Redirect 的 "redirect"）：
// 它们由各自的事务内结案与补偿处理，不在启动恢复的分派范围内。
const (
	// ReceiptActionSwitchActive 发布：路径上的符号链接从旧产物切到暂存产物。
	ReceiptActionSwitchActive = "switch_active"
	// ReceiptActionUpdateURL 改 URL：新路径激活了新产物，旧路径按 301 / 下线处置。
	ReceiptActionUpdateURL = "update_url"
	// ReceiptActionRollback 回滚：路径上的符号链接切回历史产物。
	ReceiptActionRollback = "rollback"
)

// 回执归属（publication_receipts.source_type，与 page_routes 的 CHECK 同源）。
//
// 收敛例程按它筛出「自己该管的那一批」：手工页面（page）与自动发布实例（presentation）
// 的活跃指针在不同表上，用同一套补齐逻辑会写错地方。字面量收在这里而不是各调用方
// 各写一遍 —— 两处各写一遍迟早分叉，而分叉的表现是「回执留在 pending 但没人认领」。
const (
	// ReceiptSourcePage 手工页面发布（page 模块的发布链）。
	ReceiptSourcePage = "page"
	// ReceiptSourcePresentation 自动发布实例（presentation 模块的自动发布）。
	ReceiptSourcePresentation = "presentation"
)

// PendingReceiptResp 一条未完成的回执（启动恢复的输入）。
type PendingReceiptResp struct {
	ID         string `json:"id"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	ProjectID  string `json:"projectId"`
	Path       string `json:"path"`
	Lang       string `json:"lang"`
	// FromArtifactID / ToArtifactID 切换前后的产物行 id（update_url 的 to 为空，见请求字段注释）。
	FromArtifactID string `json:"fromArtifactId"`
	ToArtifactID   string `json:"toArtifactId"`
	Action         string `json:"action"`
	// OldPath / Redirect 切换前线上路径与它的处置方式（见请求字段注释）。
	OldPath   string `json:"oldPath"`
	Redirect  bool   `json:"redirect"`
	CreatedAt string `json:"createdAt"`
}
