// Package pubcontract 定义 publication 模块对外能力。
package pubcontract

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

// 访问面切换回执的动作名（恢复流程按它分派补齐例程）。
//
// 由 publication 定义、page 消费：回执落的是这张表，动作名是这张表的词汇表，
// 调用方按名登记与查询，不各自维护一份字面量（两处各写一遍迟早分叉，
// 而分叉的表现是「回执留在 pending 但恢复例程根本不认识它」）。
const (
	ReceiptActionSwitchActive = pubdto.ReceiptActionSwitchActive
	ReceiptActionUpdateURL    = pubdto.ReceiptActionUpdateURL
	ReceiptActionRollback     = pubdto.ReceiptActionRollback
	// ReceiptActionDeactivate 下线：实体删除/退役时撤掉它全部访问路径的符号链接
	// （没有目标产物，恢复方向是"撤链"而不是"补链"）。
	ReceiptActionDeactivate = pubdto.ReceiptActionDeactivate

	// 回执归属（publication_receipts.source_type）。收敛例程按它筛出「自己该管的那一批」：
	// page 与 presentation 的活跃指针在不同表上，用同一套补齐逻辑会写错地方。
	ReceiptSourcePage         = pubdto.ReceiptSourcePage
	ReceiptSourcePresentation = pubdto.ReceiptSourcePresentation
	// ReceiptSourceProject 项目级下线来源（仅 action=deactivate，SourceID 必须是真实 project UUID）。
	// 收敛例程按来源筛选：presentation 仍只领 presentation，project 回执由 page 侧的
	// 项目退役流程消费，不会被 presentation 的收敛误处理。
	ReceiptSourceProject = pubdto.ReceiptSourceProject
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import publication/dto。
type (
	ActivateReq                      = pubdto.ActivateReq
	DeactivateReq                    = pubdto.DeactivateReq
	RedirectReq                      = pubdto.RedirectReq
	RenameReservedReq                = pubdto.RenameReservedReq
	ReserveReq                       = pubdto.ReserveReq
	DeleteRoutesReq                  = pubdto.DeleteRoutesReq
	DeleteRoutesByPresentationReq    = pubdto.DeleteRoutesByPresentationReq
	ListActivePathsByPresentationReq = pubdto.ListActivePathsByPresentationReq
	ListActivePathsReq               = pubdto.ListActivePathsReq
	IsOccupiedReq                    = pubdto.IsOccupiedReq
	RouteResp                        = pubdto.RouteResp

	// 发布回执（TX-009）的两个形状：登记入参与未结案回执视图。
	// 它们此前漏在重导出列表外，结果是 page 侧的发布账本文件被迫直接 import publication/dto ——
	// 同一模块的其余 8 个文件都走 pubcontract，只有这一处破例，属遗漏而非设计。
	BeginPublishReceiptReq = pubdto.BeginPublishReceiptReq
	ReceiptPathIdentity    = pubdto.ReceiptPathIdentity
	// ReceiptOwner 回执所属实体的不可变标识（source_type + source_id 成对）。
	ReceiptOwner       = pubdto.ReceiptOwner
	PendingReceiptResp = pubdto.PendingReceiptResp
	ReceiptsQueryReq   = pubdto.ReceiptsQueryReq
)

// ReceiptAttemptVerdict 关联回执的整体判定（四态，见 dto 定义）。
//
// 消费者必须区分"明确失败"与"证据还没到"：前者可终态化，后者必须留 pending 等下一轮。
// 类型用别名（同一类型，不需要转换）；四个状态用 const 重导出（复制常量表达式，因此
// 仍是编译期常量，可放进 switch case，也不会被调用方改写）。
type ReceiptAttemptVerdict = pubdto.ReceiptAttemptVerdict

const (
	ReceiptAttemptNoEvidence ReceiptAttemptVerdict = pubdto.ReceiptAttemptNoEvidence
	ReceiptAttemptInProgress ReceiptAttemptVerdict = pubdto.ReceiptAttemptInProgress
	ReceiptAttemptSucceeded  ReceiptAttemptVerdict = pubdto.ReceiptAttemptSucceeded
	ReceiptAttemptFailed     ReceiptAttemptVerdict = pubdto.ReceiptAttemptFailed
)

// PublicationService URL 占用、激活与回滚控制能力。
type PublicationService interface {
	// Activate 把路径占用从 reserved 切换为 active 并记录回执。
	Activate(ctx context.Context, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error)
	// Deactivate 取消路径占用（幂等）。
	Deactivate(ctx context.Context, req *pubdto.DeactivateReq) (err error)
	// Redirect 把旧路径标记为指向新路径的重定向。
	Redirect(ctx context.Context, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error)
	// RollbackReceipts 把 pending 回执批量标记为 rolled_back（启动恢复用）。
	RollbackReceipts(ctx context.Context) (count int64, err error)
	// BeginPublishReceipt 在**切换访问面之前**登记一条 pending 发布回执，返回回执 id。
	//
	// 为什么必须在切换之前：切换是访问面原子的、但不可逆的副作用；登记放在之后，
	// 崩溃窗口里就查不到「这次发布发生过」，只能人工比对链接与数据库（TX-009）。
	//
	// 附加事实的交接纪律：req 里只放**登记这一刻已经是事实**的值。
	// CapturedInputVersion 是调用方在捕获段读到的实体输入代次 ——
	// 0 = 不提供（未知，收敛退回链接身份 + 数据库归属两判据），非 0 = 可比的版本事实。
	// 调用方不得用提交段的当前版本回填这个字段。
	BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error)
	// CompletePublishReceipt 标记回执完成（数据库状态已与访问面一致）。
	CompletePublishReceipt(ctx context.Context, receiptID string) (err error)
	// AbortPublishReceipt 把回执标记为已回滚（切换尚未发生或明确失败）。
	AbortPublishReceipt(ctx context.Context, receiptID string) (err error)
	// ListPendingReceipts 列出未完成的回执（启动全量恢复扫描）。
	ListPendingReceipts(ctx context.Context) (list []pubdto.PendingReceiptResp, err error)
	// ClaimPendingReceipts 领取一批待收敛的未结案回执（收敛例程的输入）。
	//
	// 与 ListPendingReceipts 的区别是**领取**而不是**列举**：分批（Limit）+ 行锁
	// （FOR UPDATE SKIP LOCKED），多实例部署下同一瞬间只有一个实例领到同一批行。
	// 过滤条件由调用方给（归属 + 认得的动作名）—— 同一张回执表上叠着多套恢复职责。
	ClaimPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (list []pubdto.PendingReceiptResp, err error)
	// CountPendingReceipts 统计未结案回执数（只读观测：健康检查 / 后台）。
	// 与 ClaimPendingReceipts 同口径，避免「报 0 而实际有残留」。
	CountPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (n int64, err error)
	// RenameReserved 修改页面的草稿路径占用（reserved 状态改名）。
	RenameReserved(ctx context.Context, req *pubdto.RenameReservedReq) (err error)
	// ReservePath 创建草稿路径 reserved 占用（页面创建时预留，冲突返回占用错误）。
	ReservePath(ctx context.Context, req *pubdto.ReserveReq) (err error)
	// RefreshSiteFiles 依据已激活路由重写站点 sitemap.xml / robots.txt / feed.xml
	// 与自定义 404 页（dir 为空则跳过）。
	// langs 为站点启用语言（默认语言在前，多语言 P3），defaultLang 用于 x-default；
	// 二者为空时按单语言输出（与 P3 之前一致）。
	// siteLangMode 为该工程**生效**的语言 URL 方案（off / default_plain / all_prefix）：
	// 方案是工程级的值，解析入口只有 pipeline.SiteLangURLModeOf —— publication 不认识
	// project 契约，不能自己查，也不能读任何「当前站点方案」的进程级状态。
	// notFoundHTML 为站点自定义 404 页内容（projects.settings 里的 HTML）；空 = 未配置，
	// 此时删除激活目录里既有的 404.html（激活目录直接对外服务，不删等于继续返回已下线的旧页）。
	// 内容由调用方（page 装配层，持有 project 契约）传入 —— publication 不跨模块读站点设置。
	RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang, siteLangMode, notFoundHTML string) (err error)
	// DeleteRoutesByPage 清理页面全部路径占用（页面删除时释放，幂等）。
	DeleteRoutesByPage(ctx context.Context, req *pubdto.DeleteRoutesReq) (err error)
	// ListActivePathsByPresentation 返回展示实例已激活（active/redirect）的路径集合。
	//
	// 删除实例前必须按它逐个解除访问面激活：改过 URL 的实例除新路径外还有一条
	// 旧路径的 redirect 链接，只清 DB 路由行在线上的表现仍是 301 到一个死页面。
	ListActivePathsByPresentation(ctx context.Context, req *pubdto.ListActivePathsByPresentationReq) (paths []string, err error)
	// DeleteRoutesByPresentation 清理展示实例全部路径占用（实例删除时释放，幂等）。
	// 与 DeleteRoutesByPage 分开：归属列不同，合成一个可空组合参数会让
	// 「忘了传归属者」变成静默全删。
	DeleteRoutesByPresentation(ctx context.Context, req *pubdto.DeleteRoutesByPresentationReq) (err error)
	// ListActivePaths 返回页面已激活（active/redirect）的路径集合。
	//
	// 调用方（页面删除）须按这些路径解除访问面激活：/site 直接服务 active 目录的
	// 文件系统状态，只清 DB 路由行不会让已删除内容下线——符号链接还在，内容就还在。
	ListActivePaths(ctx context.Context, req *pubdto.ListActivePathsReq) (paths []string, err error)
	// IsPathOccupied 查询路径是否被其他实体占用（页面创建/发布前预检）。
	IsPathOccupied(ctx context.Context, req *pubdto.IsOccupiedReq) (occupied bool, err error)
	// ListReferencedArtifactIDs 返回全部被路由引用的产物行 ID（产物 GC 的保护集合）。
	// 路由指向的产物文件被删除意味着线上直接 404，且路由行不会因文件消失而失效。
	ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error)

	// —— 事务透传变体（AGENTS.md「写操作的事务与回滚」）——
	//
	// 同库跨模块的写必须能与调用方自己的表落在**同一个事务**里：调用方
	// （page 的建页 / 存草稿 / 改 URL / 回滚 / 删页）先开事务并设置工程作用域，
	// 再把 *gorm.DB 句柄传进来。这些方法**不新开事务、不做补偿、不回读**，
	// 失败原样返回给外层——由外层统一回滚。
	//
	// 为什么不能用「先写 A 再补偿 B」：补偿只允许用于跨库/外部系统（文件、Redis、
	// 第三方），同库的两处写用补偿意味着中途崩溃或进程被杀死时留下半截状态。
	ActivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.ActivateReq) (res *pubdto.RouteResp, err error)
	DeactivateTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeactivateReq) (err error)
	RedirectTx(ctx context.Context, tx *gorm.DB, req *pubdto.RedirectReq) (res *pubdto.RouteResp, err error)
	RenameReservedTx(ctx context.Context, tx *gorm.DB, req *pubdto.RenameReservedReq) (err error)
	ReservePathTx(ctx context.Context, tx *gorm.DB, req *pubdto.ReserveReq) (err error)
	DeleteRoutesByPageTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesReq) (err error)
	// DeleteRoutesByPresentationTx 清理展示实例全部路径占用（实例删除时释放，幂等）。
	//
	// 调用方是 presentation 的实例删除：实例行归它、page_routes 行归本模块，
	// 两处写必须同一个事务 —— 所以这里给的是事务形态而不是让调用方各提交一次。
	DeleteRoutesByPresentationTx(ctx context.Context, tx *gorm.DB, req *pubdto.DeleteRoutesByPresentationReq) (err error)

	// ReceiptAttemptVerdictTx 在**调用方事务**内判定一组关联回执是否整体成功
	// （三态：Succeeded / Failed / InProgress|NoEvidence）。
	//
	// 跨模块边界的形状：调用方（presentation 改址的旧路径处置、page 的同构收敛）
	// 持实体行锁，需要在本事务里回读 publication_receipts —— 但那张表归本模块，
	// 调用方不得直接读。这里给出**读**端口：不新开事务、不写、不回读别表；
	// ownedLangs（语言 → 调用方账本里的成功回执 id）由调用方自己提供，
	// 因此"账本归属"这一半判定仍由账本主人给出事实，本模块只判自己那张表。
	ReceiptAttemptVerdictTx(ctx context.Context, tx *gorm.DB, links map[string]string,
		ownedLangs map[string]string) (pubdto.ReceiptAttemptVerdict, error)

	// RouteOwnerTx 在**调用方事务**内读一条路径当前的占用归属（无行返回 gorm.ErrRecordNotFound）。
	//
	// 用途单一：调用方（presentation 的旧路径处置 / 删除 / 退役）持实体行锁与路径锁时，
	// 必须在这段里回读"这条路径此刻还归不归我"。归属判定放在锁外就等于没判定 ——
	// 另一个实体可以在"读完 → 动手"之间接管该路径，随后被我们删掉/覆盖。
	RouteOwnerTx(ctx context.Context, tx *gorm.DB, projectID, path string) (*pubdto.RouteOwnerResp, error)

	// MarkDeactivationCommittedTx 在**调用方业务事务内**记录"下线意图对应的业务变更已提交"。
	//
	// 与业务变更同 tx 是硬要求：标记单独提交会制造「标记说已提交、业务其实回滚了」的
	// 假事实，恢复随后把一次没发生的删除当成"该撤链接"执行掉。回执状态保持 pending
	// （否则领取口看不见它，链接就永远不会被撤），committed 只在全部链接撤完后写。
	//
	// owner 是显式的期望归属（source_type + source_id）：page id 与 presentation 实例 id
	// 都是 uuid，只校验 source_type 白名单挡不住"同工程另一实体的回执被这次标记命中"。
	MarkDeactivationCommittedTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
		projectID, receiptID string, pathArtifacts map[string]pubdto.ReceiptPathIdentity) error

	// LoadDeactivationReceiptTx 在**调用方事务内**（通常已持业务实体锁）重读一条下线回执的
	// 当前事实：receipt_state + businessCommitted + pathArtifacts。
	//
	// 必须存在的理由：ClaimPendingReceipts 是短事务 FIND ... SKIP LOCKED，没有租约，
	// 同一行可被多个收敛同时领到 —— 领取结果是快照，不能当结论。只有在业务锁之下重读
	// 本行，才能区分"业务事务还没提交"与"业务已提交、链接还没撤"。
	LoadDeactivationReceiptTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
		projectID, receiptID string) (*pubmodel.ReceiptEntity, error)
}
