package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type CurrencyRateEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	BaseCode   string    `gorm:"column:base_code;not null"`
	QuoteCode  string    `gorm:"column:quote_code;not null"`
	Rate       string    `gorm:"column:rate;not null"`
	RateAt     time.Time `gorm:"column:rate_at;not null"`
	Source     string    `gorm:"column:source;not null"`
	UpdateTime time.Time `gorm:"column:update_time;not null"`
}

func (CurrencyRateEntity) TableName() string { return "currency_rates" }

var ErrCurrencyRateNotFound = errors.New("currency rate not found")

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

func normalizeCurrencyCode(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }

type FXOrderRow struct {
	ID         uint64 `gorm:"column:id"`
	Total      int64  `gorm:"column:total"`
	FXRate     string `gorm:"column:fx_rate"`
	FXBaseCode string `gorm:"column:fx_base_code"`
}

func (m *OrderModel) ListFXOrdersByRange(ctx context.Context, projectID string, from, to time.Time,
	f OrderSalesFilter, baseCode string) (rows []FXOrderRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return nil, ErrRangeRequired
	}
	base := normalizeCurrencyCode(baseCode)
	if base == "" {
		return nil, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return salesScope(
			tx.Table(OrderEntity{}.TableName()+" AS o").
				Select("o.id, o.total, CAST(o.fx_rate AS text) AS fx_rate, o.fx_base_code").
				Where("o.fx_rate IS NOT NULL").
				Where("upper(o.fx_base_code) = ?", base),
			projectID, from, to, f,
		).Order("o.id ASC").Scan(&rows).Error
	})
	return rows, err
}

func (m *OrderModel) CountUnconvertedOrders(ctx context.Context, projectID string, from, to time.Time,
	f OrderSalesFilter, baseCode string) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	if from.IsZero() || to.IsZero() {
		return 0, ErrRangeRequired
	}
	base := normalizeCurrencyCode(baseCode)
	if base == "" {
		return 0, ErrRangeRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return salesScope(
			tx.Table(OrderEntity{}.TableName()+" AS o").
				Where("o.fx_rate IS NULL OR upper(o.fx_base_code) <> ?", base),
			projectID, from, to, f,
		).Count(&n).Error
	})
	return n, err
}
