package productcontract

// publish_record.go — 发布记录页的只读端口（B9）。
//
// 消费者是商品模块自己的页面读模型，实现方是 outbound/publishrecord 适配器
//（它再经 presentation 的 PublishRecordPort 读三张表）。为什么中间还要一层：
// 页面要的形状是「商品 → 逐语言明细」，而 presentation 给的是「实例 + 语言账本」——
// 翻译放在 outbound（internal/module/CLAUDE.md §outbound），service 只持有翻译后的端口。

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// PublishRecordReader 按商品 id 读「当前发布登记」。
type PublishRecordReader interface {
	// Langs 返回 商品 id → 逐语言发布明细。
	//
	// entityIDs 为空 = 本工程全部商品类型实例。**读不到就是错误**：这一屏的每一行
	// 都是发布事实，静默返回空表会让运营以为「一个都没发布」。
	Langs(ctx context.Context, projectID string, entityIDs []string) (map[string][]productdto.PublishRecordLangResp, error)
}
