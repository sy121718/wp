package model

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func (m *OrderModel) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		orderIDs := tx.Model(&OrderEntity{}).
			Select("id").
			Where("project_id = ?", projectID).
			Where("user_id = ?", userID).
			Where("status = ANY(string_to_array(?, ',')::text[])", strings.Join(DefaultSalesStatuses, ","))

		var hit []string
		if err := tx.Model(&OrderItemEntity{}).
			Select("id").
			Where("product_id = ?", productID).
			Where("order_id IN (?)", orderIDs).
			Limit(1).
			Find(&hit).Error; err != nil {
			return err
		}
		purchased = len(hit) > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return purchased, nil
}
