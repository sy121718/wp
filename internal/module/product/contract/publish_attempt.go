package productcontract

// 商品发布历史的只读契约；当前发布状态由 PublishRecordPort 提供。

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// PublishAttemptReader 按商品读发布历史。
type PublishAttemptReader interface {
	// PublishAttempts 读某商品最近 limit 次发布尝试（新→旧）。
	//
	// productID 必填，空串是参数错误（出口给 400）而不是空历史 —— 「没给商品」与
	// 「这个商品没发布过」是两件事。**读不到就是错误**：这一屏每行都是「发过 / 没发出去」
	// 的事实，静默返回空表会让运营以为一次都没发过。
	PublishAttempts(ctx context.Context, projectID, productID string, limit int) (res *productdto.PublishAttemptListResp, err error)
}
