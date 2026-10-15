package productmodel

// product_rating_model.go — 商品评分明细（issue #30）。
//
// 评分是**独立表 + 明细行**，不是商品上的两个列：
//
//	· 一行 = 一条评分（score 0~5）；
//	· 商品查询经 hasMany 关联 Preload 明细（一次批量拉回，不是 N+1）；
//	· 平均分与条数由明细**算出来**（投影），不落库。
//
// 为什么存明细而不是存聚合：聚合值（评分 4.5、120 条）是明细的函数。
// 存成商品列意味着每加一条评分都要回写商品行、并且丢掉明细 ——
// 将来评论域落地时无处可接，而明细表只需要换个写入方。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductRatingEntity 一条商品评分。
type ProductRatingEntity struct {
	ID        string    `gorm:"column:id;primaryKey"`
	ProjectID string    `gorm:"column:project_id;not null"`
	ProductID string    `gorm:"column:product_id;not null"`
	Score     float64   `gorm:"column:score;not null"`
	Source    string    `gorm:"column:source;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductRatingEntity) TableName() string { return "product_ratings" }

// RatingSummary 由评分明细算出的投影值（issue #30）。
//
// ok=false 表示**一条评分都没有** —— 与「平均分 0」严格区分：
// 组件据此把该商品排在评分排序的最后、并让它不入选最低评分筛选。
func RatingSummary(ratings []ProductRatingEntity) (avg float64, count int, ok bool) {
	if len(ratings) == 0 {
		return 0, 0, false
	}
	total := 0.0
	for _, r := range ratings {
		total += r.Score
	}
	return total / float64(len(ratings)), len(ratings), true
}

// RatingSummaryOf 商品自身的评分投影（明细已 Preload 时直接算）。
func (p *ProductEntity) RatingSummaryOf() (avg float64, count int, ok bool) {
	if p == nil {
		return 0, 0, false
	}
	if p.RatingCount != nil && *p.RatingCount > 0 && p.RatingAvg != nil {
		return *p.RatingAvg, *p.RatingCount, true
	}
	return RatingSummary(p.Ratings)
}

// ListRatings 取某商品的评分明细（按时间倒序：最近的在前）。
//
// projectID 由调用方给出：product_ratings 在迁移 215 名单里，缺作用域时这条查询会
// 静默返回空明细 —— 页面上表现为「评分突然全没了」，而 SQL 本身没有任何错。
// 明细空 ⇒ RatingSummary 的 ok=false ⇒ 连商品的评分标签一起消失。
func (m *Model) ListRatings(ctx context.Context, productID, projectID string) (list []*ProductRatingEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductRatingEntity{}).
			Where("product_id = ?", productID).
			Order("create_time DESC, id DESC").Find(&list).Error
	})
	return list, err
}

// RatingAgg 一个商品的评分投影（批量读回后按商品聚合的结果）。
//
// 与 RatingSummary 的关系是「同一个函数算出来的值」：批量路径按商品分组后仍然调
// RatingSummary，不做 SQL 侧 AVG —— 理由见 RatingSummariesByProducts。
type RatingAgg struct {
	// Avg 平均分（与 RatingSummary 的逐条累加逐字一致）。
	Avg float64
	// Count 评分条数（恒 > 0；**无评分的商品不进 map**）。
	Count int
}

// RatingSummariesByProducts 一次读回若干商品的评分投影（商品列表页消 N+1 用）。
//
// **为什么聚合放在 Go 侧而不是 SQL 的 GROUP BY + AVG**：口径会漂。实测（310 组
// score numeric(3,2) 样本，对照 RatingSummary 的逐条累加）：AVG(score)::float8 与逐条
// 累加有 76 例浮点不等、其中 **8 例两位小数展示不同**（例：4.49 + 1.90 —— 逐条
// 3.1950000000000003 展示 3.20，SQL 侧 3.1949999999999998 展示 3.19）。同一个商品
// 在列表页与详情页会因此显示不同的平均分，而两边各自看都对。所以这里只做
// 「一次批量读取 + 按商品分组」，聚合一律交回 RatingSummary（唯一口径）。
// 代价是回传本页商品的评分明细行：评分是运营补录数据、product_id 上有索引，
// 量级与本页商品数同阶（也与详情页读一个商品的明细同阶）。
//
// 语义：
//   - productIDs 为空（或去掉空串后为空）→ 直接返回空 map，不发 IN ()；
//   - 无评分的商品**不在 map 里**（调用方按 RatingAgg{} 处理 ⇒ HasRating=false，
//     与 ratingsOf 的零值渲染一致）；有一条 0 分的评分则 Count=1、Avg=0；
//   - 项目隔离与 ListRatings 同一份：同一张表（product_ratings）+ rls.InProjectScope；
//   - 任一批次失败返回 error（**不返回半截 map 冒充成功**）—— 列表页据此判 500，
//     而不是把「读不到」渲染成「暂无评分」。
func (m *Model) RatingSummariesByProducts(ctx context.Context, projectID string, productIDs []string) (map[string]RatingAgg, error) {
	ids := make([]string, 0, len(productIDs))
	for _, id := range productIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	out := make(map[string]RatingAgg, len(ids))
	for start := 0; start < len(ids); start += idBatchSize {
		end := min(start+idBatchSize, len(ids))
		var rows []*ProductRatingEntity
		if err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
			return tx.WithContext(ctx).Model(&ProductRatingEntity{}).
				// project_id 谓词显式写进 WHERE（与 rls.InProjectScope 叠加，不是替代）：
				// 策略在换非超级角色之前不生效（AGENTS.md DB-009），只靠作用域时本连接读得到
				// 别的工程的评分行 —— 判据见 product_rating_summary_batch_test.go 的跨工程用例。
				Where("project_id = ? AND product_id IN ?", projectID, ids[start:end]).
				// 组内顺序与 ListRatings 逐字一致（create_time DESC, id DESC）：
				// 浮点累加与顺序有关，换序会让同一商品的均值在两个路径上差 1 ulp。
				Order("product_id, create_time DESC, id DESC").
				Find(&rows).Error
		}); err != nil {
			return nil, err
		}
		grouped := make(map[string][]ProductRatingEntity, len(rows))
		for _, row := range rows {
			grouped[row.ProductID] = append(grouped[row.ProductID], *row)
		}
		for productID, items := range grouped {
			if avg, count, ok := RatingSummary(items); ok {
				out[productID] = RatingAgg{Avg: avg, Count: count}
			}
		}
	}
	return out, nil
}

// CreateRating 写入一条评分（projectID 由调用方从商品带出，保持与商品同工程）。
func (m *Model) CreateRating(ctx context.Context, e *ProductRatingEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductRatingEntity{}).Create(e).Error
	})
}

// GetRating 按 id 取一条评分（校验归属用）。
//
// projectID 由调用方给出：product_ratings 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetRating(ctx context.Context, id, projectID string) (e *ProductRatingEntity, err error) {
	var row ProductRatingEntity
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductRatingEntity{}).Where("id = ?", id).Take(&row).Error
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteRating 删除一条评分。
//
// projectID 由调用方给出：product_ratings 在迁移 215 名单里，缺作用域时 DELETE 会静默
// 匹配 0 行 —— service 已经先 GetRating 确认过行存在，所以这里不会报「找不到」，
// 表现是「点了删除，刷新一条没少」。
func (m *Model) DeleteRating(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductRatingEntity{}).
			Where("id = ?", id).Delete(&ProductRatingEntity{}).Error
	})
}
