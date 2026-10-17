package pagedto

// Package pagedto 发布链路请求/响应结构。

// LangPublishResp 一键发布里单个语言的结果。
type LangPublishResp struct {
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
	PageID  string            `json:"pageId"`
	Results []LangPublishResp `json:"results"`
	// Published 成功激活的语言数（与 Results 中 status=ok 的条数一致，便于前端速览）。
	Published int `json:"published"`
	// Skipped 被本页排除而跳过的语言数（status=skipped）。与 Published 分开计数：
	// 「跳过了几种语言」是这一页的产出范围，不是本批的失败。
	Skipped int `json:"skipped"`
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

// PublicationIssueResp 一条异常激活链接（巡检报告项）。
type PublicationIssueResp struct {
	URLPath string `json:"urlPath"`
	Link    string `json:"link"`
	Reason  string `json:"reason"`
}

// PublicationAuditResp 激活面巡检报告。
type PublicationAuditResp struct {
	Checked int                    `json:"checked"`
	Issues  []PublicationIssueResp `json:"issues"`
	Healthy bool                   `json:"healthy"`
	// Orphans 磁盘上存在、但没有任何属主认领的产物目录（IDX-015 的反向对账）。
	// 与 Issues 分开：Issues 是「线上立刻 404」的故障，孤儿只是占磁盘，处置优先级不同。
	Orphans []OrphanArtifactResp `json:"orphans"`
	// OrphanChecked 参与反向对账的磁盘产物目录数（与 Checked 区分：后者是链接数）。
	OrphanChecked int `json:"orphanChecked"`
}

// OrphanArtifactResp 磁盘有、数据库无主的产物目录。
type OrphanArtifactResp struct {
	Hash  string `json:"hash"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

// GCRecoveredArtifactResp 单条回收结果。
type GCRecoveredArtifactResp struct {
	ID           string `json:"id"`
	ArtifactHash string `json:"artifactHash"`
	Lang         string `json:"lang"`
	// Action: would_delete / deleted / kept_shared / delete_failed / state_failed
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// GCArtifactsResp 产物回收报告。
type GCArtifactsResp struct {
	RetentionDays int                       `json:"retentionDays"`
	DryRun        bool                      `json:"dryRun"`
	Scanned       int                       `json:"scanned"`
	Deleted       int                       `json:"deleted"`
	SkippedShared int                       `json:"skippedShared"`
	Failed        int                       `json:"failed"`
	Items         []GCRecoveredArtifactResp `json:"items"`

	// 内容对象（content_objects）孤儿回收：产物行回收后，它引用的共享内容对象可能
	// 已无人引用 —— 同一趟里做标记清除（审计 IDX-016）。四个字段与上面的产物计数
	// 并列而非混在一起：两者一个是文件、一个是内容对象的 Locator 投影，混count会读不清。
	OrphanObjects          int64 `json:"orphanObjects"`
	ObjectsDeleted         int64 `json:"objectsDeleted"`
	ObjectsSkippedExternal int   `json:"objectsSkippedExternal"`
	ObjectsFailed          int   `json:"objectsFailed"`
}
