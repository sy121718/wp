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

// RouteOwnerResp 一条路径当前的占用归属（RouteOwnerTx 的投影）。
//
// 只给"这条路径归谁、激活的是哪份产物"，不含业务派生字段：调用方（presentation 的
// 旧路径处置/删除/退役）用它做**锁内归属复验**，需要的是可判等的事实，不是视图。
type RouteOwnerResp struct {
	ProjectID      string
	Path           string
	RouteKind      string
	PageID         *string
	PresentationID *string
	ArtifactID     *string
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
	// ReceiptActionDeactivate 下线：实体即将被删除/退役，它占用的**全部**访问路径都要
	// 撤掉符号链接。与 switch_active 分开取名的理由：
	//   - 它没有目标产物（下线的结果是不存在，不是另一份字节）；
	//   - 恢复方向相反：switch_active 是"把链接补到新产物"，deactivate 是"把链接撤掉"；
	//   - 它需要额外的一条持久事实 businessCommitted（业务 DB 变更是否已提交），
	//     因为"链接该撤"的前提是"业务真的提交了"，而这条判据只在 deactivate 上有意义。
	// publication_receipts.action 无 CHECK 约束（见 init_builder_schema.sql 的建表语句），
	// 新动作名不需要迁移。
	ReceiptActionDeactivate = "deactivate"
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
	// ReceiptSourceProject 项目级下线（无 page / presentation 归属的 redirect 链接撤除）。
	//
	// 只有 deactivate 允许这个来源，且 SourceID 必须是真实 project UUID、PageID 与
	// PresentationID 都必须为空 —— 这是**唯一**新增来源，不是"任意 source 泛化"：
	// 允许任意 source 会让收敛例程按来源筛选时把别人的回执当成自己的。
	ReceiptSourceProject = "project"
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
	OldPath  string `json:"oldPath"`
	Redirect bool   `json:"redirect"`
	// CapturedInputVersion 登记这条回执时**捕获段**读到的实体输入代次（pages.input_version /
	// presentation_instances.input_version，迁移 699）。
	//
	// 0 = 未知（旧回执 / 调用方不提供该事实）：收敛例程据此只能做「链接身份 + 数据库归属」
	// 判定，**不得**把它当成「版本有效」。非 0 才是可比对的版本事实 —— 比对口径是
	// 实体行锁内读到的当前代次，二者相等才说明这次激活编译的是最新的一代输入。
	//
	// 这个值只能来自登记时调用方交上来的编译捕获事实；从回执创建时刻反查实体当前版本
	// 顶替它，等于把「提交时看到的版本」冒充「编译时捕获的版本」，收敛会错判为有效。
	CapturedInputVersion int64 `json:"capturedInputVersion"`
	// OldPaths 本次改址要处置的全部旧路径（见请求字段注释）；空 = 只有 OldPath 一条。
	OldPaths []string `json:"oldPaths,omitempty"`
	// SuccessfulReceipts 本次改址关联的成功回执（语言 → switch_active 回执 id）。
	//
	// 只有 update_url 会带它。消费者必须在**实体行锁内**回读这些 id 的最终状态：
	// 全部 committed 才证明"本次激活真的成功过"，才允许执行旧路径处置。
	// 空 = 没有关联事实（历史回执）：保守处理，pending 不执行处置。
	SuccessfulReceipts map[string]string `json:"successfulReceipts,omitempty"`
	// FromReceiptID 本次 attempt 开始时账本上已成功的回执 id（原样投影登记时的事实）。
	//
	// 三种取值都要被差别对待，不能被归成一种：
	//   - "0"：捕获段确认没有前次成功身份；
	//   - ""：未知（历史回执 / 旧消费者），消费方按最保守口径（退回 artifact 身份核验）；
	//   - 十进制 id：前一次的成功身份。恢复时「当前成功身份 == 它」表示我没被新身份取代。
	// 恢复例程**不得**在恢复时回查当前账本值来"补造"它 —— 那会把今天的成功身份冒充成
	// 我登记时的身份，于是任何一次后来的成功都会被读成"我就是它"。
	FromReceiptID string `json:"fromReceiptId"`
	// Owner 本回执所属实体（source_type + source_id）。恢复侧用它做归属校验，
	// 不靠"领到之后再去猜这是谁的回执"。
	Owner ReceiptOwner `json:"owner"`
	// PathArtifacts 每条待下线路径的归属事实（action=deactivate 专用，见请求字段）。
	PathArtifacts map[string]ReceiptPathIdentity `json:"pathArtifacts,omitempty"`
	// BusinessCommitted 该 deactivate 意图对应的**业务 DB 变更**是否已在业务事务里提交。
	//
	// 只对 action=deactivate 有意义，且它不是"状态"而是"事实之一"：
	//   - false + pending：业务事务没提交（或从未走到提交）→ 链接必须保持原样；
	//   - true  + pending：业务已提交、链接还没撤 → 恢复例程要把链接撤掉后结案；
	//   - committed：链接已撤、业务已提交，全部完成。
	//
	// 恢复例程**不得**把 false 直接当成"业务一定会回滚"：那个业务事务可能正在写、
	// 尚未提交。判据必须以"拿到同一把业务锁之后重读本行"为准（见 presentation 的
	// 下线恢复实现），拿领取快照当结论会让原业务成功而意图丢失。
	BusinessCommitted bool   `json:"businessCommitted"`
	CreatedAt         string `json:"createdAt"`
}
