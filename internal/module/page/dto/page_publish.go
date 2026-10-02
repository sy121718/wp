// Package pagedto 发布链路请求/响应结构。
package pagedto

// BuildReq 基于当前草稿构建产物（暂存，不激活）。
type BuildReq struct {
	ID              string `json:"id" binding:"required"`
	ExpectedVersion int64  `json:"expectedVersion"`
	// Lang 构建语言（多语言 P2）：空 = 全局默认语言（sys_config 的 i18n 组 default_lang）。
	Lang string `json:"lang"`
}

// PageBuildJobReq 构建队列（source_type=page）任务的执行上下文（审计 ARCH-04）。
//
// 它是「任务上下文」在 page 侧的投影，三样一起决定这次重建做什么：
//   - Lang：任务冻结的构建语言（完整语言码）。逐语言一行任务（迁移 307 的待办键含 lang），
//     因此每条任务只负责一种语言；空串只可能来自 ARCH-04 之前入队的存量行，
//     RunPageBuildJob 对它按站点启用语言集合处理（与同步路径一致），而不是当成默认语言。
//   - Intent：构建意图 manual / dependency（见 pagecontract 的 BuildIntent* 常量）。
//   - DraftVersion / BuildInputHash：入队时冻结的输入版本，用于去重与审计。
type PageBuildJobReq struct {
	ID   string `json:"id" binding:"required"`
	Lang string `json:"lang"`
	// Intent 空串按 dependency 处理（与 build_jobs.intent 的默认值同口径）。
	Intent         string `json:"intent"`
	DraftVersion   int64  `json:"draftVersion"`
	BuildInputHash string `json:"buildInputHash"`
}

// PublishReq 激活暂存产物。
type PublishReq struct {
	ID string `json:"id" binding:"required"`
	// Lang 发布语言：必须与构建语言一致，否则暂存产物与激活路径不匹配。
	Lang string `json:"lang"`
	// AllLangs 一键发布全部启用语言（多语言开关开启时的发布口径）：
	//
	//	按站点启用语言清单逐语言「构建 + 激活」，一次请求把设置页配置的每种语言
	//	各编译一份并上线。置真时 Lang 被忽略（语言集合以发布时刻的清单为准，
	//	默认语言在前）。单语言失败不阻断其余语言，错误按语言逐条回传。
	AllLangs bool `json:"allLangs"`
}

// LangPublishResult 一键发布里单个语言的结果。
type LangPublishResult struct {
	// Lang 本条结果对应的语言（完整语言码）。
	Lang string `json:"lang"`
	// Status ok / failed / skipped（单语言失败不阻断其余语言，失败原因在 Error）。
	// skipped = 该语言已被本页排除（作者主动决策，不是失败）。
	Status string `json:"status"`
	// ActiveHash 激活产物哈希（成功时回传；失败为空）。
	ActiveHash string `json:"activeHash,omitempty"`
	// Error 失败原因（成功为空；原文只进本字段，供后台展示与运维定位）。
	Error string `json:"error,omitempty"`
}

// PublishAllResp 一键发布全部启用语言的聚合结果。
type PublishAllResp struct {
	PageID  string              `json:"pageId"`
	Results []LangPublishResult `json:"results"`
	// Published 成功激活的语言数（与 Results 中 status=ok 的条数一致，便于前端速览）。
	Published int `json:"published"`
	// Skipped 被本页排除而跳过的语言数（status=skipped）。与 Published 分开计数：
	// 「跳过了几种语言」是这一页的产出范围，不是本批的失败。
	Skipped int `json:"skipped"`
}

// RollbackReq 回滚到指定历史产物。
type RollbackReq struct {
	ID         string `json:"id" binding:"required"`
	TargetHash string `json:"targetHash" binding:"required"`
	// Lang 回滚语言（多语言 P3）：留空时取目标产物冻结的 Manifest.lang，
	// 二者都为空才回退站点默认语言。回滚只作用于该语言的激活状态与路由。
	Lang string `json:"lang"`
}

// UpdateURLReq 修改访问路径并按策略处理旧路径。
type UpdateURLReq struct {
	ID           string `json:"id" binding:"required"`
	NewPath      string `json:"newPath" binding:"required"`
	WithRedirect bool   `json:"withRedirect"`
	// Lang 目标语言（多语言 P2）：决定新路径的实际访问前缀，空 = 站点默认语言。
	Lang string `json:"lang"`
}

// PublishResp 发布链路操作结果。
type PublishResp struct {
	PageID      string `json:"pageId"`
	Status      string `json:"status"`
	StagedHash  string `json:"stagedHash,omitempty"`
	ActiveHash  string `json:"activeHash,omitempty"`
	OldPath     string `json:"oldPath,omitempty"`
	DraftPath   string `json:"draftPath"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

// RebuildArtifactReq 按产物元数据重建丢失的产物文件（灾难恢复）。
type RebuildArtifactReq struct {
	ArtifactID string `json:"artifactId" binding:"required"`
}

// RebuildArtifactResp 产物重建结果。
type RebuildArtifactResp struct {
	ArtifactID   string `json:"artifactId"`
	Lang         string `json:"lang"`
	Path         string `json:"path"`
	ExpectedHash string `json:"expectedHash"`
	ActualHash   string `json:"actualHash"`
	// Restored 产物文件现已可用（文件本就存在，或重建后 hash 一致）。
	Restored bool `json:"restored"`
	// HashMatched 重建结果与元数据记录的 hash 一致（hash 相同才算真的恢复）。
	HashMatched bool `json:"hashMatched"`
	// AlreadyThere 文件原本就在，未执行编译（幂等短路）。
	AlreadyThere bool `json:"alreadyThere"`
	// Reason 重建产物与元数据不一致时的差异说明。
	Reason string `json:"reason"`
}

// PublicationIssue 一条异常激活链接（巡检报告项）。
type PublicationIssue struct {
	URLPath string `json:"urlPath"`
	Link    string `json:"link"`
	Reason  string `json:"reason"`
}

// PublicationAuditResp 激活面巡检报告。
type PublicationAuditResp struct {
	Checked int                `json:"checked"`
	Issues  []PublicationIssue `json:"issues"`
	Healthy bool               `json:"healthy"`
	// Orphans 磁盘上存在、但没有任何属主认领的产物目录（IDX-015 的反向对账）。
	// 与 Issues 分开：Issues 是「线上立刻 404」的故障，孤儿只是占磁盘，处置优先级不同。
	Orphans []OrphanArtifact `json:"orphans"`
	// OrphanChecked 参与反向对账的磁盘产物目录数（与 Checked 区分：后者是链接数）。
	OrphanChecked int `json:"orphanChecked"`
}

// OrphanArtifact 磁盘有、数据库无主的产物目录。
type OrphanArtifact struct {
	Hash  string `json:"hash"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

// GCArtifactsReq 产物回收请求。
type GCArtifactsReq struct {
	// RetentionDays 保留窗口（天）：早于 now-retentionDays 创建、且不再被任何指针引用的
	// 产物才可回收。留空或 <=0 按默认 30 天。
	RetentionDays int `json:"retentionDays"`
	// DryRun 只列出候选、不实际删除。留空视为 true（安全默认：必须显式传 false 才真删）。
	DryRun *bool `json:"dryRun"`
}

// GCRecoveredArtifact 单条回收结果。
type GCRecoveredArtifact struct {
	ID           string `json:"id"`
	ArtifactHash string `json:"artifactHash"`
	Lang         string `json:"lang"`
	// Action: would_delete / deleted / kept_shared / delete_failed / state_failed
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// GCArtifactsResp 产物回收报告。
type GCArtifactsResp struct {
	RetentionDays int                   `json:"retentionDays"`
	DryRun        bool                  `json:"dryRun"`
	Scanned       int                   `json:"scanned"`
	Deleted       int                   `json:"deleted"`
	SkippedShared int                   `json:"skippedShared"`
	Failed        int                   `json:"failed"`
	Items         []GCRecoveredArtifact `json:"items"`

	// 内容对象（content_objects）孤儿回收：产物行回收后，它引用的共享内容对象可能
	// 已无人引用 —— 同一趟里做标记清除（审计 IDX-016）。四个字段与上面的产物计数
	// 并列而非混在一起：两者一个是文件、一个是内容对象的 Locator 投影，混count会读不清。
	OrphanObjects          int64 `json:"orphanObjects"`
	ObjectsDeleted         int64 `json:"objectsDeleted"`
	ObjectsSkippedExternal int   `json:"objectsSkippedExternal"`
	ObjectsFailed          int   `json:"objectsFailed"`
}
