package pagemodel

// page_stale_model.go — 待重建（stale）页面的**只读反查投影**（反查面，不写库）。
//
// 现象（本文件补的缺口）：pages.stale 一直只是一个布尔列 + 页面列表行里的一枚徽标。
// 「这次改动影响哪几个页面」在后台答不出来 —— 已有的几个只读块（content 的
// articleStaleImpact、block 的 blockStaleImpact）给的是**全局 stale 计数 + 一份走
// page.List 全量扫描再过滤出来的清单**，它回答的是「有多少页待重建」，而不是
// 「是哪些页、最近一次被标记是什么时候」。而「最近被标记的那几页」正是「这次改动
// 影响谁」在数据上唯一可用的近似（标记时把 update_time 一起写成了标记时刻，
// 见 MarkStaleByDependency / MarkStaleByIDs / MarkStaleForBlock 的 UPDATE 子句）。
//
// 本文件只提供**只读**入口，不改变任何标记 / 构建 / 发布行为：
//   - ListStale / CountStale —— 按工程列出 / 统计待重建页面（id / title / path / update_time）；
//   - ListBriefsByIDs —— 按 id 集合取同样的投影（写侧拿到扇出返回的 ids 后，
//     用它把 id 翻译成人能读的「标题 / 路径」）。
//
// limit 与排序一律以参数传入，本文件不写死「取前几条」「按什么排」——
// 那是调用方的口径，抄一份到这里就是第二份真相（改一处不漏另一处的典型形态）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// staleTitleExpr 页面标题的取数表达式（draft_document.settings.seo.title）。
//
// 为什么标题要从 JSONB 里挖：**pages 表没有 title 列**（同一结论见
// service/page_redirect.go 的 redirectOwnerLabels：「页面没有标题列，用草稿路径辨识」）。
// 作者填的标题只存在于文档的 SEO 段落里，因此这里是标题取数的**唯一**一份口径：
// 它只出现在 stalePageSelect() 的投影里，排序一律走投影别名（ORDER BY title），
// 不再把表达式抄进 ORDER BY —— 抄一份就等于给「标题到底怎么取」留下第二个答案。
//
// 取不到（没填 / 只有空白）时得到空串，由调用方回落到路径 —— 不在这里编造标题。
const staleTitleExpr = "COALESCE(NULLIF(BTRIM(draft_document #>> '{settings,seo,title}'), ''), '')"

// 待重建清单的排序键（白名单，非业务条件）。
const (
	// StaleOrderUpdateTime 按标记时间排序（调用方通常取降序：「最近这次改动影响的在前」）。
	StaleOrderUpdateTime = "update_time"
	// StaleOrderDraftPath 按草稿路径排序。
	StaleOrderDraftPath = "draft_path"
	// StaleOrderTitle 按页面标题排序（标题缺失的排在空串那一端）。
	StaleOrderTitle = "title"
	// StaleOrderID 按页面 id 排序（uuid，仅用于需要与 SQL 完全一致次序的场景）。
	StaleOrderID = "id"
)

// 只读清单的 limit 归一化区间。
const (
	// DefaultStaleListLimit 调用方没给 limit（<=0）时的默认条数。
	DefaultStaleListLimit = 50
	// MaxStaleListLimit 单次只读清单的条数上限。
	MaxStaleListLimit = 500
)

// ErrInvalidStaleOrder 排序键不在白名单里。
//
// 排序键会进 ORDER BY，属于 SQL 标识符位置，不能由调用方拼接 ——
// 白名单外一律拒绝（而不是退回默认值静默按别的列排：那样调用方拿到的次序
// 与它要求的不一致，且没有任何信号）。
var ErrInvalidStaleOrder = errors.New("page: 待重建清单的排序键不在白名单里")

// staleOrderKeys 排序键白名单（只用来判「这个键认不认」，值就是投影里的输出列名）。
//
// 白名单校验与拼接分两步：拼接一律用**校验通过的键本身**作为 ORDER BY 列名，
// 而所有投影都恰好以这个名字给出（id / draft_path / title / update_time）——
// 于是「表达式只写一次、排序列名来自白名单」两件事同时成立。
var staleOrderKeys = map[string]struct{}{
	StaleOrderUpdateTime: {},
	StaleOrderDraftPath:  {},
	StaleOrderTitle:      {},
	StaleOrderID:         {},
}

// NormalizeStaleOrder 校验并归一化排序键：空串取默认（标记时间），白名单外报错。
//
// 导出它是因为 limit / 排序的口径必须只有一份：service 在进 model 之前就要能判定
// 「这个请求合不合法」，不能自己再列一次白名单（两份白名单早晚会分叉）。
func NormalizeStaleOrder(orderBy string) (string, error) {
	key := strings.TrimSpace(orderBy)
	if key == "" {
		return StaleOrderUpdateTime, nil
	}
	if _, ok := staleOrderKeys[key]; !ok {
		return "", ErrInvalidStaleOrder
	}
	return key, nil
}

// NormalizeStaleListLimit 归一化 limit：<=0 取默认，超上限封顶。
func NormalizeStaleListLimit(limit int) int {
	if limit <= 0 {
		return DefaultStaleListLimit
	}
	if limit > MaxStaleListLimit {
		return MaxStaleListLimit
	}
	return limit
}

// StalePageRow 待重建页面（或任意页面）的只读投影。
//
// Title / Path 的关系：Title 是作者在文档里填的 SEO 标题（可能为空），Path 是
// 「已上线路径优先、回落草稿路径」的可读标识（口径见 service 的 staleDisplayPath）。
// 两者都给出来，展示层自己决定优先显示哪个 —— 读侧不再替它编一个标题。
type StalePageRow struct {
	ID        string `gorm:"column:id"`
	ProjectID string `gorm:"column:project_id"`
	Title     string `gorm:"column:title"`
	DraftPath string `gorm:"column:draft_path"`
	// ActivePath 已上线路径；未发布时为 nil（此时唯一可读标识是 DraftPath）。
	ActivePath *string   `gorm:"column:active_path"`
	Stale      bool      `gorm:"column:stale"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

// stalePageSelect 只读投影的 SELECT 片段（与 StalePageRow 的列一一对应）。
//
// 与 staleTitleExpr 同源：标题表达式只在这里出现一次。
func stalePageSelect() string {
	return "id, project_id, " + staleTitleExpr + " AS title, draft_path, active_path, stale, update_time"
}

// staleOrderClause 拼 ORDER BY 子句（列名是校验过的排序键，方向由布尔决定）。
//
// 末尾的 id ASC 是**必需的次级键**：一次改动往往在同一毫秒里标记一批页面，
// 没有稳定次序时「前 N 条」每次都可能不同 —— 影响面清单会在两次刷新之间抖动，
// 读的人会以为系统在乱标。
//
// column 是投影的输出列名而不是任意 SQL：所有查询都把它作为 SELECT 别名给出，
// 因此 PostgreSQL 会解析成输出列（不依赖输入表里是否有同名列）。
func staleOrderClause(column string, descending bool) string {
	dir := "ASC"
	if descending {
		dir = "DESC"
	}
	return column + " " + dir + ", id ASC"
}

// ListStale 在**指定工程作用域内**列出待重建页面（只读）。
//
// limit 与排序由调用方给出（orderBy 走 NormalizeStaleOrder 的白名单）。
// projectID 必填：pages 在迁移 215 里带 FORCE 策略，谓词读会话变量 app.project_id，
// 不设作用域在换非超级角色后**静默返回 0 行**（fail closed 不报错）——
// 那会让影响面看起来是「没有待重建页面」。
func (m *Model) ListStale(ctx context.Context, projectID string, limit int, orderBy string, descending bool) (list []StalePageRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	key, oerr := NormalizeStaleOrder(orderBy)
	if oerr != nil {
		return nil, oerr
	}
	column := key
	limit = NormalizeStaleListLimit(limit)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select(stalePageSelect()).
			Where("project_id = ? AND deleted_at IS NULL AND stale = true", projectID).
			Order(staleOrderClause(column, descending)).
			Limit(limit).
			Scan(&list).Error
	})
	return list, err
}

// CountStale 统计**指定工程内**待重建页面数（只读）。
//
// 与 ListStale 的匹配条件必须逐字一致（同 stale = true / deleted_at IS NULL）：
// 计数与清单取自两个查询时，条件分叉会表现为「页面说 3 个，清单列了 5 行」。
func (m *Model) CountStale(ctx context.Context, projectID string) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Where("project_id = ? AND deleted_at IS NULL AND stale = true", projectID).
			Count(&n).Error
	})
	return n, err
}

// ListBriefsByIDs 按 id 集合返回页面只读投影（工程作用域内、未删除）。
//
// 不筛 stale：调用方用它把**扇出返回的 id 集合**翻译成标题 / 路径，那个集合的语义
// 由调用方持有（例如「刚被这次改动标记的页面」）。在读侧再筛一次 stale 等于把
// 「标记是否已经生效」这个前提偷偷写进查询 —— 前提一旦不成立，影响面会静默变空。
//
// ids 为空返回空切片（不是错误）：没有受影响的页面不是异常。
func (m *Model) ListBriefsByIDs(ctx context.Context, projectID string, ids []string, limit int, orderBy string, descending bool) (list []StalePageRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	key, oerr := NormalizeStaleOrder(orderBy)
	if oerr != nil {
		return nil, oerr
	}
	column := key
	limit = NormalizeStaleListLimit(limit)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select(stalePageSelect()).
			Where("project_id = ? AND id IN ? AND deleted_at IS NULL", projectID, ids).
			Order(staleOrderClause(column, descending)).
			Limit(limit).
			Scan(&list).Error
	})
	return list, err
}
