package pubdto

import "strings"

// ActivateReq 激活请求：把 reserved 占用升级为 active 并绑定产物。
//
// 归属者二选一：PageID（手工页面）或 PresentationID（自动发布实例）。
// page_routes 的 CHECK 约束要求二者恰好一个非空（init_builder_schema.sql），
// 「二选一」无法用 binding:"required" 表达，由 service 层校验（parseRouteOwner）。
type ActivateReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	PageID    string `json:"pageId" form:"pageId"`
	// PresentationID 自动发布实例归属（presentation_instances.id）。
	// 详情页发布实例的 URL 占用此前根本没登记进 page_routes，导致页面侧
	// 占用预检查不出详情页抢路径；这个字段是补上那半边的入口。
	PresentationID string `json:"presentationId" form:"presentationId"`
	ArtifactID     string `json:"artifactId" form:"artifactId" binding:"required"`
	Action         string `json:"action" form:"action"`
}

// DeactivateReq 取消激活请求。
//
// 归属者可空：指定时只取消该归属者的占用（展示实例必须指定 —— 它的行
// page_id 为 NULL，历史判据 page_id IS NOT NULL 够不着它）；都不指定时
// 保持「取消该路径上任意页面占用」的既有语义，page 侧调用逐字不变。
type DeactivateReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	PageID    string `json:"pageId" form:"pageId"`
	// PresentationID 展示实例归属；指定时按它精确匹配（page_id 为 NULL 的行）。
	PresentationID string `json:"presentationId" form:"presentationId"`
}

// RedirectReq 旧路径重定向标记请求；ArtifactID 允许为空（重定向产物不入库）。
//
// 归属者二选一同 ActivateReq：改 URL 后旧路径要由「原来的主人」标记为
// redirect，归属者不对就会被判成抢占他人路径。
type RedirectReq struct {
	ProjectID      string `json:"projectId" form:"projectId" binding:"required"`
	OldPath        string `json:"oldPath" form:"oldPath" binding:"required"`
	PageID         string `json:"pageId" form:"pageId"`
	PresentationID string `json:"presentationId" form:"presentationId"`
	ArtifactID     string `json:"artifactId" form:"artifactId"`
}

// RenameReservedReq 草稿路径占用改名请求（reserved → reserved）。
type RenameReservedReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
	OldPath   string `json:"oldPath" form:"oldPath" binding:"required"`
	NewPath   string `json:"newPath" form:"newPath" binding:"required"`
	// OnlyReserved 只迁移 reserved 行，跳过本页 active/redirect 行。
	// 多语言 P3：改某语言 URL 时，其他语言的 active 行绝不能被顺带改名
	//（否则 /zh-CN/about 的激活行会被迁到新路径，线上路由丢失）。
	OnlyReserved bool `json:"onlyReserved" form:"onlyReserved"`
}

// ReserveReq 创建草稿路径 reserved 占用（页面创建时预留）。
type ReserveReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

// DeleteRoutesReq 清理页面全部路径占用（页面删除时释放）。
type DeleteRoutesReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

// ListActivePathsReq 查询页面已激活的路径集合（页面删除时清理访问面文件用）。
type ListActivePathsReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	PageID    string `json:"pageId" form:"pageId" binding:"required"`
}

// IsOccupiedReq 查询路径是否被其他实体占用（用于页面创建/发布前的预检）。
//
// 两个排除项各排除自己那一行：改 URL 时「自己占着旧路径」不算冲突。
// 展示实例改 URL 必须传 ExcludePresentationID —— 它的行 page_id 为 NULL，
// 用 ExcludePageID 排除不掉自己，会把自身占用判成冲突。
type IsOccupiedReq struct {
	ProjectID     string `json:"projectId" form:"projectId" binding:"required"`
	Path          string `json:"path" form:"path" binding:"required"`
	ExcludePageID string `json:"excludePageId" form:"excludePageId"`
	// ExcludePresentationID 排除展示实例自身的占用行。
	ExcludePresentationID string `json:"excludePresentationId" form:"excludePresentationId"`
}

// ListActivePathsByPresentationReq 查询展示实例已激活的路径集合（实例删除时清理访问面文件用）。
type ListActivePathsByPresentationReq struct {
	ProjectID      string `json:"projectId" form:"projectId" binding:"required"`
	PresentationID string `json:"presentationId" form:"presentationId" binding:"required"`
}

// DeleteRoutesByPresentationReq 清理展示实例全部路径占用（实例删除时释放）。
//
// 与 DeleteRoutesByPage 并存而不是合并：两者的归属列不同，合并成一个可空
// 组合参数会让「忘了传归属者」变成静默全删，比多一个方法危险得多。
type DeleteRoutesByPresentationReq struct {
	ProjectID      string `json:"projectId" form:"projectId" binding:"required"`
	PresentationID string `json:"presentationId" form:"presentationId" binding:"required"`
}

// ReceiptAttemptVerdict 一组关联回执（语言 → 同批 switch_active 回执 id）的整体判定。
//
// 放在 dto 而不是 model：它是**调用方可见的判定结果**，contract 与 service 都要用它，
// 而 contract 不 import 别人的 model（AGENTS.md 分层判据）。
//
// 必须是四态而不是布尔：「明确失败」与「证据还没到」对调用方的动作完全相反 ——
// 前者可以终态化（rolled_back），后者必须保留 pending 下一轮再判。
type ReceiptAttemptVerdict int

const (
	// ReceiptAttemptNoEvidence 关联为空或全部 id 非法：保留 pending。
	ReceiptAttemptNoEvidence ReceiptAttemptVerdict = iota
	// ReceiptAttemptInProgress 关联回执仍 pending / 暂缺：保留 pending，下一轮再判。
	ReceiptAttemptInProgress
	// ReceiptAttemptSucceeded 全部 committed 且账本承认其中至少一条。
	ReceiptAttemptSucceeded
	// ReceiptAttemptFailed 明确失败（rolled_back / superseded / 账本归属不符）：可以终态化。
	ReceiptAttemptFailed
)

// BeginPublishReceiptReq 登记一条「即将切换访问面」的发布回执。
//
// FromArtifactID 是本次发布前该语言的活跃产物（首次发布为空）；ToArtifactID 是本次
// 要激活的产物。恢复流程靠它们判断「符号链接现在指着谁」，因此两者都要如实填写。
type BeginPublishReceiptReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Path      string `json:"path" form:"path" binding:"required"`
	// PageID / PresentationID 二选一：手工页面发布走前者，自动发布实例走后者。
	PageID         string `json:"pageId" form:"pageId"`
	PresentationID string `json:"presentationId" form:"presentationId"`
	// Owner 显式归属（可选）。只有一种用途：**项目级下线**（action=deactivate 且
	// 该链接没有 page / presentation 归属，例如 redirect 符号链接），此时
	// Owner.SourceType 必须是 'project'、SourceID 必须是真实 project UUID，
	// 且 PageID / PresentationID 必须为空。
	//
	// 其它任何组合仍然走原来的 parseRouteOwner（page / presentation 二选一）：
	// 这不是"任意 source 泛化"，是为了让"没有实体归属的链接"也有可校验的 owner，
	// 否则回执只能挂在某个假实体上，收敛时就会被那个实体的例程误领。
	Owner          *ReceiptOwner `json:"owner,omitempty" gorm:"-"`
	FromArtifactID string        `json:"fromArtifactId" form:"fromArtifactId"`
	// ToArtifactID 本次要激活的产物行 id。
	//
	// 例外：Action 为 update_url 时允许为空 —— 改 URL 的产物是**按新路径**现编译的
	// （canonicalPath 进 Manifest 并参与 hash），登记回执时（切换之前）拿不到它的
	// 产物行 id。该形态的恢复改用「新路径上的产物 canonicalPath 是否等于新路径」判定。
	ToArtifactID string `json:"toArtifactId" form:"toArtifactId"`
	// Lang 构建语言（多语言站点一个页面每个语言一条回执）。
	Lang   string `json:"lang" form:"lang"`
	Action string `json:"action" form:"action"`
	// OldPath 切换前该语言的线上路径（改 URL 的旧路径 / 回滚前的路径）。
	//
	// 放进回执是因为恢复要用它处置旧路径（301 或下线）：从新路径反推不出来，
	// 而那一刻数据库里的激活记录还是旧值也会随时间被后续操作改掉。
	OldPath string `json:"oldPath" form:"oldPath"`
	// Redirect 旧路径是否应登记为 301 重定向（update_url 专用）。
	Redirect bool `json:"redirect" form:"redirect"`
	// CapturedInputVersion 调用方在**捕获段**读到的实体输入代次（pages.input_version /
	// presentation_instances.input_version，迁移 699）。收敛例程靠它判断「这条回执的
	// 激活编译的是哪一代输入」：与实体行锁内的当前代次不等 = 线上那份产物已过期。
	//
	// 0 = 调用方不提供该事实（未知）。未知不是有效：收敛侧只在值 > 0 时否决，值为 0
	// 时退回「链接身份 + 数据库归属」两判据，绝不会把 0 当成「与当前版本相同」。
	//
	// 调用方必须交**编译时捕获**的那一代（page 主链是捕获段读到的 page.InputVersion），
	// 不得用提交段的当前值回填 —— 两者之间输入还可能被推进，冒充出来的版本会让
	// 收敛把一份过期产物判成最新。
	CapturedInputVersion int64 `json:"capturedInputVersion" form:"capturedInputVersion"`
	// OldPaths 本次改址要处置的**全部**旧路径（多语言一站一条回执时，每种语言一条）。
	//
	// OldPath 只放首条以兼容既有消费者（page 的 update_url 恢复读它）；完整清单进这里，
	// 否则恢复只能处置第一条 —— 其余语言的旧链接既不会被 301、也不会被下线，
	// 与持久化的意图不符。空 = 只有 OldPath 一条（历史回执形态）。
	OldPaths []string `json:"oldPaths" form:"oldPaths"`
	// SuccessfulReceipts 本次改址关联的成功回执：语言 → 该语言 switch_active 回执 id。
	//
	// 只对 update_url 有意义：一次改址会先为本批每种语言登记一条 switch_active，再登记
	// 这一条 update_url，把那些 id 记在这里。恢复/收敛处理这条 update_url 时，**必须**
	// 先按这些 id 回读它们的最终状态 —— 全部 committed 才证明"本次激活真的成功过"，
	// 才允许执行旧路径处置。不看时间、不看 URL 相等：同一路径连续两次 attempt 的
	// URL 完全相同，只有回执 id 能区分。
	//
	// 空 = 没有关联事实（历史回执或不带改址的登记），消费者按最保守口径处理。
	SuccessfulReceipts map[string]string `json:"successfulReceipts" form:"-" gorm:"-"`
	// FromReceiptID 本次 attempt 开始时，该语言账本上**已经成功**的那条回执 id。
	//
	// 为什么必须有它：同一个产物（同一条 artifact 行、同一个 URL）可以被连续两次 attempt
	// 激活 —— 只比 artifact / URL 无法区分"我取代了谁"。恢复时必须能回答两个问题：
	//   1) 当前成功身份 == 我登记时的 FromReceiptID → 我是"接替已知前人"的那一次，
	//      允许前向补齐（哪怕期间有别的 attempt 也走过，只要没留下成功身份）；
	//   2) 当前成功身份 == 我自己的 ID → 已经落过账，幂等补处置即可；
	//   3) 其它（一个**新的**成功身份）→ 明确被取代，不许覆盖。
	// 只靠 From/To 产物 id 达不到这点：同产物多 attempt 时两者完全相同。
	//
	// 取值口径（必须如实照抄，不许在登记时"补造"）：
	//   - "0"：本次捕获段确认**没有**前次成功身份（首次发布 / 账本行还没写 receipt_id）；
	//   - ""：调用方没有提供这个事实（历史回执、旧消费者）—— 未知，消费方按最保守口径处理，
	//     退回 artifact 身份核验，不得当成"没有前次成功"；
	//   - 非空非 "0"：十进制回执 id，即账本行上的 receipt_id。
	FromReceiptID string `json:"fromReceiptId" form:"fromReceiptId"`
	// PathArtifacts 每条待下线路径在登记时刻的**己方身份**（action=deactivate 专用）。
	//
	// 取值是"这条路径此刻归谁"的精确事实，不是拼接串：
	//   - ArtifactID：该路径在账本里对应的产物行 id（历史行可能为空）；
	//   - ReceiptID：该路径对应账本行的成功 attempt 身份（历史行可能为空）；
	//   - Locator：访问面上这条链接登记时的定位器（provider + key），
	//     用于恢复时判定"链接是否还是我登记时那一条"，与产物 id 互不替代。
	//
	// 恢复侧只用它做**归属判定**：链接/路由仍指向这里的身份才允许处置；
	// 已被别的实体接管（对不上）就保留 pending、不动别人的链接。
	PathArtifacts map[string]ReceiptPathIdentity `json:"pathArtifacts,omitempty" gorm:"-"`
}

// ReceiptOwner 回执所属实体的不可变标识（source_type + source_id 恰好一对）。
//
// 为什么不让调用方分开传两个字符串：source_type 与 source_id 必须成对出现，
// 分开传时"传了 presentation 的 id 却标成 page"这种错配不会被编译器发现，
// 后果是恢复例程领到一个 id 对不上的回执，然后静默跳过（回执永远 pending）。
// 两个 owner（page / presentation）用同一个结构与同一个构造函数，协议只有一份。
type ReceiptOwner struct {
	// SourceType publication_receipts.source_type（'page' / 'presentation' / 'project'）。
	SourceType string `json:"sourceType"`
	// SourceID 实体 id（page、presentation 实例，或 project UUID）。
	SourceID string `json:"sourceId"`
}

// IsZero 未设置（调用方必须显式给出归属）。
func (o ReceiptOwner) IsZero() bool {
	return o.SourceType == "" || o.SourceID == ""
}

// IsProject 项目级归属（无 page / presentation 实体的链接下线）。
func (o ReceiptOwner) IsProject() bool {
	return strings.TrimSpace(o.SourceType) == ReceiptSourceProject
}

// ReceiptPathIdentity 一条访问路径在回执登记时刻的归属事实。
//
// 三个字段各自独立、可为空，**不许**用分隔符把它们拼成一个字符串，也不许用其中一个
// 顶替另一个：产物的内容寻址让同一条链接可以指向别人的同一份字节，仅凭 artifact id
// 无法证明归属；而没有 artifact id 的历史行只能靠 locator 与 receipt id 辨认。
type ReceiptPathIdentity struct {
	// ArtifactID 该路径对应的产物行 id。
	ArtifactID string `json:"artifactId,omitempty"`
	// ReceiptID 该路径账本行的成功 attempt 身份（十进制回执 id 串，历史行空）。
	ReceiptID string `json:"receiptId,omitempty"`
	// Locator 登记时访问面链接的实际定位器。
	Locator string `json:"locator,omitempty"`
}

// ReceiptsQueryReq 领取 / 统计未结案回执的过滤条件（收敛例程与可观测接口共用同一口径）。
//
// 过滤条件为什么由调用方传：同一张表上叠着多套恢复职责 —— 手工页面（page）与自动发布
// 实例（presentation）各自的补齐例程认得的动作不同，路由回执（activate / redirect）
// 又由各自的事务内结案处理。领到不属于自己的回执只会被反复领取再跳过，还会把真正
// 待办的行挤出单批上限（饥饿）—— 所以「领取口径」必须与「谁能收敛」严格一致。
type ReceiptsQueryReq struct {
	// SourceType 回执归属（'page' / 'presentation'）；空 = 不限。
	SourceType string `json:"sourceType" form:"sourceType"`
	// Actions 认得的动作名；空 = 不限（不筛就领到全部，谨慎使用）。
	Actions []string `json:"actions" form:"actions"`
	// Limit 单批上限（领取用；统计忽略）。<= 0 视为「不领任何行」。
	Limit int `json:"limit" form:"limit"`
}

// SeoAuditReq SEO 体检的入参。
//
// 键是 `project`，json 与 form 取同名：三种传输（JSON / urlencoded / multipart）
// 用同一套键，端点不因 Content-Type 换形状。绑定失败时 binding 已写好 400，调用方只 return。
type SeoAuditReq struct {
	Project string `json:"project" form:"project"`
}

// ResolveProject 取最终的工程标识：body 去空白后非空则用它，否则回落到查询串。
//
// 去空白与取值顺序都在这里，handler 不读 c.Query。
func (r *SeoAuditReq) ResolveProject(query *SeoAuditReq) string {
	if r != nil {
		if v := strings.TrimSpace(r.Project); v != "" {
			return v
		}
	}
	if query != nil {
		return strings.TrimSpace(query.Project)
	}
	return ""
}
