package pagedto

// Package pagedto 发布链路请求/响应结构。

// BuildReq 基于当前草稿构建产物（暂存，不激活）。
type BuildReq struct {
	ID              string `json:"id" form:"id" binding:"required"`
	ExpectedVersion int64  `json:"expectedVersion" form:"expectedVersion"`
	// Lang 构建语言（多语言 P2）：空 = 全局默认语言（sys_config 的 i18n 组 default_lang）。
	Lang string `json:"lang" form:"lang"`
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
	ID   string `json:"id" form:"id" binding:"required"`
	Lang string `json:"lang" form:"lang"`
	// Intent 空串按 dependency 处理（与 build_jobs.intent 的默认值同口径）。
	Intent         string `json:"intent" form:"intent"`
	DraftVersion   int64  `json:"draftVersion" form:"draftVersion"`
	BuildInputHash string `json:"buildInputHash" form:"buildInputHash"`
}

// PublishReq 激活暂存产物。
type PublishReq struct {
	ID string `json:"id" form:"id" binding:"required"`
	// Lang 发布语言：必须与构建语言一致，否则暂存产物与激活路径不匹配。
	Lang string `json:"lang" form:"lang"`
	// AllLangs 一键发布全部启用语言（多语言开关开启时的发布口径）：
	//
	//	按站点启用语言清单逐语言「构建 + 激活」，一次请求把设置页配置的每种语言
	//	各编译一份并上线。置真时 Lang 被忽略（语言集合以发布时刻的清单为准，
	//	默认语言在前）。单语言失败不阻断其余语言，错误按语言逐条回传。
	AllLangs bool `json:"allLangs" form:"allLangs"`
}

// RollbackReq 回滚到指定历史产物。
type RollbackReq struct {
	ID         string `json:"id" form:"id" binding:"required"`
	TargetHash string `json:"targetHash" form:"targetHash" binding:"required"`
	// Lang 回滚语言（多语言 P3）：留空时取目标产物冻结的 Manifest.lang，
	// 二者都为空才回退站点默认语言。回滚只作用于该语言的激活状态与路由。
	Lang string `json:"lang" form:"lang"`
}

// UpdateURLReq 修改访问路径并按策略处理旧路径。
type UpdateURLReq struct {
	ID           string `json:"id" form:"id" binding:"required"`
	NewPath      string `json:"newPath" form:"newPath" binding:"required"`
	WithRedirect bool   `json:"withRedirect" form:"withRedirect"`
	// Lang 目标语言（多语言 P2）：决定新路径的实际访问前缀，空 = 站点默认语言。
	Lang string `json:"lang" form:"lang"`
}

// RebuildArtifactReq 按产物元数据重建丢失的产物文件（灾难恢复）。
type RebuildArtifactReq struct {
	ArtifactID string `json:"artifactId" form:"artifactId" binding:"required"`
}

// GCArtifactsReq 产物回收请求。
type GCArtifactsReq struct {
	// RetentionDays 保留窗口（天）：早于 now-retentionDays 创建、且不再被任何指针引用的
	// 产物才可回收。留空或 <=0 按默认 30 天。
	RetentionDays int `json:"retentionDays" form:"retentionDays"`
	// DryRun 只列出候选、不实际删除。留空视为 true（安全默认：必须显式传 false 才真删）。
	DryRun *bool `json:"dryRun" form:"dryRun"`
}
