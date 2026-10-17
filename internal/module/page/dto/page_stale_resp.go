package pagedto

import "go_wp/pkg/utils"

// StalePageResp 待重建页面的只读投影（列表项）。
//
// Title 与 Path 是两件事：Title 是作者在文档 SEO 段里填的标题（**可能为空**——
// pages 表没有 title 列），Path 是「已上线路径优先、回落草稿路径」的可读标识。
// 展示层按 Title 非空与否决定显示哪一个，读侧不替它编一个标题。
type StalePageResp struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName,omitempty"`
	Title       string `json:"title,omitempty"`
	// Path 可读标识：已上线路径优先，其次草稿路径（与 block/content 两个只读块的
	// 「页面可读标识」口径一致，避免同一个页面在三处显示成三个名字）。
	Path string `json:"path"`
	// Published 是否已上线（ActivePath 非空）。页面上用它区分「已发布但有更新未发布」
	// 与「从未发布」—— 两者的处置方式不同，合并成一句会误导人。
	Published bool `json:"published"`
	// Stale 是否待重建。按 ids 反查时该字段来自数据库现状，可能为 false
	// （调用方传进来的 id 集合与 stale 列是两件事）。
	Stale bool `json:"stale"`
	// UpdatedAt 最近一次标记 / 变更时刻（页面列表用它排在「最近影响的在前」）。
	UpdatedAt utils.JSONTime `json:"updatedAt"`
	// RebuildFailedAt / RebuildFailedStage 最近一次**自动重建失败**的时刻与阶段。
	//
	// 为什么它与 Stale 不是同一件事：stale=true 有两种含义 ——「还没轮到重建」与
	// 「重建过了但失败了」。前者等着就好，后者要人查日志。只给布尔值，读的人只能靠猜。
	// 重建成功后由 ClearRebuildFailure 清空（成功必须清：留着旧时刻会让刚恢复的页面
	// 继续显示「失败」，比不显示更糟）。
	//
	// 阶段是闭集（plan / build），**不含错误原文**：原文可能带 SQL / 路径 / 内部标识，
	// 后台页面不得直出内部错误（AGENTS.md 红线），它只进结构化日志（带 page_id 可定位）。
	RebuildFailedAt    *utils.JSONTime `json:"rebuildFailedAt,omitempty"`
	RebuildFailedStage string          `json:"rebuildFailedStage,omitempty"`
}

// StalePageListResp 待重建清单（Total 是完整计数，Pages 是按 Limit 截断后的清单）。
//
// Total 与 len(Pages) 是两个数：只给 Pages 会让人把「清单长度」当成影响面大小
// （被静默截断的清单看起来是完整的）。
type StalePageListResp struct {
	Pages     []StalePageResp `json:"pages"`
	Total     int             `json:"total"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated"`
}

// StaleImpactSummaryResp 一次写操作的影响面摘要（写侧回执 / 结构化日志共用）。
//
// Total 是本次受影响的页面总数，Pages 是其中最多 Limit 条（按标记时间降序）的
// 人类可读样本 —— 只有总数时读的人不知道「是哪些页面」；只有样本时又会把
// 样本长度当成总数。
type StaleImpactSummaryResp struct {
	Total     int             `json:"total"`
	Pages     []StalePageResp `json:"pages"`
	Limit     int             `json:"limit"`
	Truncated bool            `json:"truncated"`
}
