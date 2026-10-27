package presentationcontract

// publish_attempt.go — 发布历史的只读端口（迁移 673）。
//
// 与 publish_record.go 的分工：那个回答「现在发布成什么样」（当前指针 + 账本 + 产物版本），
// 这个回答「都试过哪几次、哪次没成、谁点的、为什么」。前者读三张状态表，后者读只增的
// 尝试台账，两者互不替代 —— 一次失败发布会把状态表原样留在上一版，只有本端口能说出
// 「前一小时有三条 failed」。
//
// 接口给的是「一页 + 游标」而不是「全部」：台账只增不删（用户明确要求全留），某实例
// 反复重发几十次是常态。全量返回等于让页面把没被看的历史也读一遍。
//
// 读的口径是**实体**（entityType + entityID）而不是页面实例：一个商品可以同时挂详情页与
// 归档页两张实例，运营问的是「这个商品发布过几次」，把两张页面的尝试拆进两个抽屉没人看得懂。

import (
	"context"
	"time"
)

// PublishAttemptRow 一次发布尝试。
type PublishAttemptRow struct {
	ID int64
	// Trigger 触发源：create / manual / template_switch / update_url / rollback /
	// override / auto_rebuild（取值与迁移 673 的 CHECK 逐字一致）。
	Trigger string
	// Outcome success / failed。
	Outcome   string
	ErrorText string
	// Langs 本次涉及的语言（失败时 = 尝试的范围）。
	Langs []string
	// Attempts 冲突重试次数（>1 说明有编译结果被丢弃重来）。
	Attempts int
	// ArtifactVersion / ArtifactHash 成功后指向的产物；失败时 0 / 空串。
	ArtifactVersion int64
	ArtifactHash    string
	// OperatorID / OperatorName 操作人快照；0 / 空串 = 系统触发（自动重建）。
	OperatorID   int64
	OperatorName string
	DurationMs   int64
	// CreatedAt 尝试发生的时刻。
	CreatedAt time.Time
}

// PublishAttemptCursor 倒序游标：上一页最后一行的 (create_time, id)。
//
// 不用偏移量：台账只增，翻页期间的插入会让 OFFSET 重复或跳过行。
type PublishAttemptCursor struct {
	CreatedAt time.Time
	ID        int64
}

// PublishAttemptPage 一页尝试记录。
type PublishAttemptPage struct {
	// Rows 本页行（新→旧）。
	Rows []PublishAttemptRow
	// Total 该实体的全部尝试条数（页面显示「共 N 次」）。
	Total int64
	// HasMore 是否还有更早的行。
	HasMore bool
	// Next 下一页游标（HasMore 为真时有效）。
	Next PublishAttemptCursor
}

// PublishAttemptPort 按实体读发布尝试。
type PublishAttemptPort interface {
	// PublishAttempts 读一个实体的发布尝试，before 为零值表示从最新开始。
	//
	// entityType 空串由实现取默认（商品）；entityID 必填，空串是参数错误（400）而不是
	// 空历史 —— 「没给实体」与「这个实体没发布过」是两件事。
	//
	// limit 由调用方给上限；limit <= 0 时由实现取一个默认值。**读不到就是错误**：
	// 这一屏的每一行都是「发过 / 没发出去」的事实，静默返回空会让「没有历史」与
	// 「读失败」看起来一样。
	PublishAttempts(ctx context.Context, projectID, entityType, entityID string, limit int) (res PublishAttemptPage, err error)
}