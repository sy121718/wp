// Package analyticscontract 定义 analytics 模块对外契约。
package analyticscontract

import (
	"context"

	analyticsdto "go_wp/internal/module/analytics/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import analytics/dto。
type (
	CollectReq  = analyticsdto.CollectReq
	SummaryReq  = analyticsdto.SummaryReq
	SummaryResp = analyticsdto.SummaryResp
	DailyCount  = analyticsdto.DailyCount
	PathCount   = analyticsdto.PathCount
	RankCount   = analyticsdto.RankCount
)

// AnalyticsService 访问统计能力。
//
// 两个方向截然不同的出口：
//   - Collect 是**公开打点**（/analytics/collect）：输入完全不可信、静默失败
//     （访客页面绝不能因为一次统计上报失败而受影响）；
//   - Summary 是**后台只读查询**：给后台统计页与只读 API 用，只聚合不写库。
//
// 这里没有「删除统计」「手工补一条」这类方法：计数是事实流水，
// 能被改写的计数等于没有计数。
type AnalyticsService interface {
	// Collect 记录一次页面浏览（公开打点，静默失败：坏请求与写库失败都不返回错误）。
	Collect(ctx context.Context, req *analyticsdto.CollectReq) (err error)
	// Summary 按天 / 按路径聚合浏览数（后台只读，时间范围与分页由请求指定）。
	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}

// TrafficReader 访问统计的只读视图：给 AI 工具的窄门。
//
// 为什么不直接复用 AnalyticsService：那一把钥匙里还有 Collect —— 它是**写入**入口
// （虽然只写统计流水）。工具由模型驱动，给它写入能力意味着「AI 往统计里塞一条」
// 在某次无关改动里变得可能，而统计数据是事实流水，被污染的计数等于没有计数。
type TrafficReader interface {
	// Summary 按天 / 按路径聚合浏览数（后台只读）。
	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}
