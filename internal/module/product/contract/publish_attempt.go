package productcontract

// publish_attempt.go — 发布历史（迁移 671）的只读端口。
//
// 消费者是商品模块自己的页面（发布记录页的「发布历史」面板），实现方是
// outbound/publishrecord 适配器：它把 presentation 的台账行翻成这一屏要的形状
//（时间 / 结果 / 触发源 / 操作人 / 语言 / 产物），翻译放在 outbound
//（internal/module/CLAUDE.md §outbound）。
//
// 与 PublishRecordReader 的分工：那个回答「现在发布成什么样」，这个回答「都试过哪几次」。
//
// **接口给一页而不是全部**：台账只增不删（用户要求全留），反复重发几十次是常态。
// 取多少条由调用方给（页面默认 10，点「加载更多」翻倍），服务端不再维护游标 ——
// 台账只增，从最新取前 N 条在交互式点击里是幂等的，而游标会逼前端存一个隐形状态。
//
// 只声明读方法：控制器拿到它也只能读。

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
