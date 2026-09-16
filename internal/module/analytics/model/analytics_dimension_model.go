package analyticsmodel

// analytics_dimension_model.go — 来源域 / 设备分类 / 语言三个维度的排行聚合（读路径）。
//
// 这三个维度的值从打点第一天起就在落库（见 service/analytics_collect.go），
// 但一直没有读取方：存储与写入成本已经付了，洞察拿不到。本文件补上读路径。
//
// 为什么读明细而不是预聚合：见 service.Summary 里那段形态选择的注释（要点是
// 预聚合表的 scope 受迁移 170 的 CHECK 约束、第 4 列 path 会被重载，
// 而维度排行的 Top-N 形状与路径排行在明细分支里已经在做的事同量级）。

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// 维度标识（service 只传这三个常量；其它任何值都被 RankByDimension 拒绝）。
const (
	// DimensionReferrer 来源域（referrer_host，打点时已归一化为域名）。
	DimensionReferrer = "referrer"
	// DimensionUA 设备分类（ua_class，服务端按请求头判定的粗粒度分类）。
	DimensionUA = "ua"
	// DimensionLang 页面语言（lang）。
	DimensionLang = "lang"
)

// dimensionColumns 维度 → page_views 列名的白名单。
//
// **为什么是 map 而不是拼字符串**：列名在 SQL 里是标识符，占位符绑不了它，
// 想让它来自变量就只有白名单一条路。任何形式的拼接（哪怕调用方「保证」合法）
// 都把一次调用方失误升级成 SQL 注入。白名单之外的值一律返回错误，不查库。
//
// 只列这三列也是刻意的：能进排行的必须是**已落库的派生值**（BIZ-8 的隐私边界）。
// session_id / visitor_hash / ip_hash 同样是列，但它们只参与去重计数 ——
// 把哈希做成排行维度展示，等于给「同一个人还去过哪些页面」提供入口。
var dimensionColumns = map[string]string{
	DimensionReferrer: "referrer_host",
	DimensionUA:       "ua_class",
	DimensionLang:     "lang",
}

// DimensionRow 某个维度取值的一行聚合。
type DimensionRow struct {
	Value    string
	Views    int64
	Visitors int64
}

// CountByDimension 按某维度聚合窗口内的浏览数与独立访客数（浏览数降序取前 limit 条）。
//
// 排序必须**确定**，所以次级键是取值本身（views DESC, value ASC）：
// 只按 views 排序时，并列行的先后由执行计划决定，而结果要被 LIMIT 截断 ——
// 截断点一旦落在并列块中间，「哪几条留在榜上」就成了执行计划的产物：
// 同一个窗口刷新两次可能给出不同的 Top-N。这不是分页独有的问题，
// Top-N 同样有（只是漏的是榜尾的条目，比翻页重复更难察觉）。
// GROUP BY 单列后取值唯一，(views, value) 因此构成全序，排序结果可复现。
//
// 空值**不排除**：referrer_host=” 是「没有来源」（直接访问 / 从地址栏输入），
// ua_class=” 是「UA 缺失」，lang=” 是「页面没上报语言」—— 各自都代表一批真实访问。
// 过滤掉它们会让排行的 views 之和小于总 PV，运营看到的是「数据丢了」而不是「有些访问没有来源」；
// 展示层负责把空值渲染成占位文案（见 internal/templates/admin/analytics.html）。
func (m *Model) CountByDimension(ctx context.Context, projectID string, from, to time.Time,
	dimension string, limit int) (rows []DimensionRow, err error) {
	column, ok := dimensionColumns[dimension]
	if !ok {
		// 不静默返回空榜：那会让一次拼错的调用看起来像「这个维度没有数据」。
		return nil, fmt.Errorf("未知的统计维度: %q", dimension)
	}
	if limit <= 0 {
		// limit 的归一化（默认值与上限）在 service，那里才是业务口径；
		// 这里只挡住 LIMIT 0 这类必然为空的调用，避免它被误读成「这个维度没有数据」。
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select(column+" AS value, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group(column).Order("views DESC, value ASC").
			Limit(limit).Scan(&rows).Error
	})
	return rows, err
}
