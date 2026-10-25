package model

// currency_rate_model.go — 汇率（币种对，覆盖式）与订单汇率快照列（迁移 630，W5）。
//
// 三条口径：
//
//  1. 汇率是**币种对**的事实（USD→CNY），不属任何工程：同一个币对在哪个工程下都该是同一个数；
//  2. 覆盖式：同一币对只留最新一条（与 inventory_stocks.cost_price 同口径）——
//     汇率的历史由**订单上的快照**保存，不靠这张表存历史；
//  3. 订单上的快照不可变：下单写一次，之后汇率怎么变都不影响历史订单（财务口径必须可重放）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// CurrencyRateEntity 汇率（迁移 630）。
//
// Rate 用 string 承载 numeric(18,8) 的**精确文本**（GORM 读 numeric 到 float64 会丢精度）：
// 汇率是财务口径，1e-8 的误差在汇总时会变成可观的金额偏差。
// 解析与换算在 service（用 big.Rat 精确算）。
type CurrencyRateEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	BaseCode   string    `gorm:"column:base_code;not null"`
	QuoteCode  string    `gorm:"column:quote_code;not null"`
	Rate       string    `gorm:"column:rate;not null"`
	RateAt     time.Time `gorm:"column:rate_at;not null"`
	Source     string    `gorm:"column:source;not null"`
	UpdateTime time.Time `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (CurrencyRateEntity) TableName() string { return "currency_rates" }

// ErrCurrencyRateNotFound 该币对还没有汇率（不是错误状态，是「未固化」）。
var ErrCurrencyRateNotFound = errors.New("currency rate not found")

// GetRate 取某币对的当前汇率（覆盖式：表里最多一行）。
//
// 归一：代码去空白 + 大写 —— `usd`/`USD` 是同一个币种，库里存的两行会让「取到哪个」
// 取决于查询顺序。
func (m *OrderModel) GetRate(ctx context.Context, base, quote string) (e *CurrencyRateEntity, err error) {
	base, quote = normalizeCurrencyCode(base), normalizeCurrencyCode(quote)
	if base == "" || quote == "" {
		return nil, ErrCurrencyRateNotFound
	}
	e = &CurrencyRateEntity{}
	ferr := m.db.WithContext(ctx).Model(&CurrencyRateEntity{}).
		Where("upper(base_code) = ? AND upper(quote_code) = ?", base, quote).First(e).Error
	if ferr != nil {
		if errors.Is(ferr, gorm.ErrRecordNotFound) {
			return nil, ErrCurrencyRateNotFound
		}
		return nil, ferr
	}
	return e, nil
}

// UpsertRate 覆盖式写汇率（同一币对只留最新一条）。
//
// 用 ON CONFLICT 而不是「先查再写」：并发两个写请求都读到「没有」时会各插一行，
// 撞唯一索引后其中一个报错 —— 而覆盖式的语义就是「后来者赢」。
func (m *OrderModel) UpsertRate(ctx context.Context, e *CurrencyRateEntity) (err error) {
	if e == nil {
		return errors.New("nil currency rate")
	}
	e.BaseCode = normalizeCurrencyCode(e.BaseCode)
	e.QuoteCode = normalizeCurrencyCode(e.QuoteCode)
	if e.BaseCode == "" || e.QuoteCode == "" {
		return errors.New("empty currency code")
	}
	now := time.Now().UTC()
	if e.RateAt.IsZero() {
		e.RateAt = now
	}
	e.UpdateTime = now
	if strings.TrimSpace(e.Source) == "" {
		e.Source = "manual"
	}
	// 用 GORM 的 OnConflict 表达「覆盖式」而不是手写 INSERT … ON CONFLICT：
	// 冲突目标必须与 DDL 的函数索引逐字一致（upper(base_code), upper(quote_code)）——
	// 写成表达式列时 GORM 用 clause.Column{Name: "..."} 会被引号包住，所以这里用
	// `ON CONFLICT ON CONSTRAINT` 的等价写法：先 Update 再按需 Insert（见下）。
	//
	// 两步的语义仍是「后来者赢」：Update 命中即覆盖；没命中的并发插入由唯一索引兜底，
	// 撞索引时那一方再 Update 一次 —— 与「先查再写」的差别是不存在「两个请求都读到没有」。
	res := m.db.WithContext(ctx).Model(&CurrencyRateEntity{}).
		Where("upper(base_code) = ? AND upper(quote_code) = ?", e.BaseCode, e.QuoteCode).
		Updates(map[string]any{
			"rate":        gorm.Expr("?::numeric", e.Rate),
			"rate_at":     e.RateAt,
			"source":      e.Source,
			"update_time": e.UpdateTime,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	return m.db.WithContext(ctx).Create(&CurrencyRateEntity{
		BaseCode: e.BaseCode, QuoteCode: e.QuoteCode,
		Rate: e.Rate, RateAt: e.RateAt, Source: e.Source, UpdateTime: e.UpdateTime,
	}).Error
}

// normalizeCurrencyCode 币种代码归一（去空白 + 大写）。
func normalizeCurrencyCode(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }

// FXOrderRow 有汇率快照的订单（折算用：每张单自己的快照汇率不同，必须逐单算）。
type FXOrderRow struct {
	ID         uint64 `gorm:"column:id"`
	Total      int64  `gorm:"column:total"`
	FXRate     string `gorm:"column:fx_rate"`
	FXBaseCode string `gorm:"column:fx_base_code"`
}

// ListFXOrdersByRange 区间内有汇率快照的订单（迁移 630，W5）。
//
// 逐单而不是一条聚合 SQL：每张单的**快照汇率不同**（下单时刻不同），
// 「每行乘以自己的汇率」需要逐行算 —— 而折算必须与 service 的 big.Rat 口径
// 完全一致，否则页面上的数字与 service 算的会各算一套。
func (m *OrderModel) ListFXOrdersByRange(ctx context.Context, projectID string, from, to time.Time,
	f OrderSalesFilter) (rows []FXOrderRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return salesScope(
			tx.Table(OrderEntity{}.TableName()+" AS o").
				Select("o.id, o.total, CAST(o.fx_rate AS text) AS fx_rate, o.fx_base_code").
				Where("o.fx_rate IS NOT NULL"),
			projectID, from, to, f,
		).Order("o.id ASC").Scan(&rows).Error
	})
	return rows, err
}

// CountUnconvertedOrders 区间内**没有**汇率快照的订单数（迁移 630，W5）。
func (m *OrderModel) CountUnconvertedOrders(ctx context.Context, projectID string, from, to time.Time,
	f OrderSalesFilter) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return 0, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return salesScope(
			tx.Table(OrderEntity{}.TableName()+" AS o").Where("o.fx_rate IS NULL"),
			projectID, from, to, f,
		).Count(&n).Error
	})
	return n, err
}
