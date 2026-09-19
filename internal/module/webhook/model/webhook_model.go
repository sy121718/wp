package webhookmodel

// webhook_model.go — webhook 域的表访问单元（端点白名单 + 投递日志两张表）。
//
// 与其它模块 model 同一形状：只暴露具名方法，不暴露 DB 句柄；
// service 层经这些方法完成全部持久化。

import (
	"context"
	"time"

	"gorm.io/gorm"

	webhookenums "go_wp/internal/module/webhook/enums"
)

// WebhookEndpointEntity 外部集成端点（管理员预注册的白名单条目）。
//
// SecretCipher 存 HMAC 签名密钥的加密文本（pkg/crypto，装配期注入 app.secret 加密），
// 明文密钥永不落库、不出 service 层。
type WebhookEndpointEntity struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	EventType    string `gorm:"column:event_type;size:128;not null;index"`
	TargetURL    string `gorm:"column:target_url;size:1024;not null"`
	SecretCipher string `gorm:"column:secret_cipher;size:512;not null"`
	Description  string `gorm:"column:description;size:512"`
	Status       int    `gorm:"column:status;not null;default:1;index"`
	// 时间列与全库口径一致：timestamptz + time.Time（迁移 212 把建表时的 bigint 秒改过来）。
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (WebhookEndpointEntity) TableName() string { return "webhook_endpoints" }

// WebhookDeliveryEntity 一次投递的日志（入队即建 pending 行，worker 回写结果）。
type WebhookDeliveryEntity struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement"`
	EndpointID     uint64 `gorm:"column:endpoint_id;not null;index"`
	EventType      string `gorm:"column:event_type;size:128;not null;index"`
	Payload        string `gorm:"column:payload;not null"`
	Status         string `gorm:"column:status;size:16;not null;default:'pending';index"`
	Attempts       int    `gorm:"column:attempts;not null;default:0"`
	ResponseStatus int    `gorm:"column:response_status"`
	LastError      string `gorm:"column:last_error;size:1024"`
	// 时间列同 webhook_endpoints：timestamptz + time.Time。
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (WebhookDeliveryEntity) TableName() string { return "webhook_deliveries" }

// WebhookModel webhook 域的表访问单元（管本模块 2 张表）。
type WebhookModel struct{ db *gorm.DB }

// NewWebhookModel 构造。
func NewWebhookModel(db *gorm.DB) *WebhookModel { return &WebhookModel{db: db} }

// tx 内部句柄。
func (m *WebhookModel) tx(ctx context.Context) *gorm.DB { return m.db.WithContext(ctx) }

// ---- 端点 ----

// CreateEndpoint 新建端点。
func (m *WebhookModel) CreateEndpoint(ctx context.Context, e *WebhookEndpointEntity) error {
	return m.tx(ctx).Create(e).Error
}

// GetEndpoint 按主键取端点。
func (m *WebhookModel) GetEndpoint(ctx context.Context, id uint64) (e *WebhookEndpointEntity, err error) {
	e = &WebhookEndpointEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ListEndpoints 列出端点（onlyEnabled 为 true 时只取启用项）。
func (m *WebhookModel) ListEndpoints(ctx context.Context, eventType string, onlyEnabled bool) (list []*WebhookEndpointEntity, err error) {
	q := m.tx(ctx).Model(&WebhookEndpointEntity{})
	if eventType != "" {
		q = q.Where("event_type = ?", eventType)
	}
	if onlyEnabled {
		q = q.Where("status = ?", 1)
	}
	err = q.Order("id ASC").Find(&list).Error
	return list, err
}

// UpdateEndpoint 更新端点字段。
func (m *WebhookModel) UpdateEndpoint(ctx context.Context, id uint64, fields map[string]any) error {
	return m.tx(ctx).Model(&WebhookEndpointEntity{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteEndpoint 删除端点。
func (m *WebhookModel) DeleteEndpoint(ctx context.Context, id uint64) error {
	return m.tx(ctx).Where("id = ?", id).Delete(&WebhookEndpointEntity{}).Error
}

// Transaction 起事务并把句柄交给调用方编排（AGENTS.md「写操作的事务与回滚」）。
//
// 边界由 service 决定：一次事件的 N 条投递日志必须同事务落库（见 DispatchEvent）。
// 队列入队（asynq/Redis）是跨系统写，**不进**这个事务 —— 为什么见 webhook_replay.go。
func (m *WebhookModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ---- 投递 ----

// CreateDelivery 新建投递日志。
func (m *WebhookModel) CreateDelivery(ctx context.Context, e *WebhookDeliveryEntity) error {
	return m.tx(ctx).Create(e).Error
}

// CreateDeliveriesTx 在外部事务内批量新建投递日志（一次事件的扇出）。
//
// 逐条 Create 各自提交时，中途失败会留下「部分端点有日志、部分没有」：
// 少了日志的那些端点**永远不会**收到这次事件 —— 没有任何东西会再来派发它。
func (m *WebhookModel) CreateDeliveriesTx(ctx context.Context, tx *gorm.DB, list []*WebhookDeliveryEntity) error {
	if len(list) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Create(&list).Error
}

// GetDelivery 按主键取投递。
func (m *WebhookModel) GetDelivery(ctx context.Context, id uint64) (e *WebhookDeliveryEntity, err error) {
	e = &WebhookDeliveryEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// UpdateDeliveryResult 回写投递结果（无条件写；仅供不可重试的确定性改动使用）。
func (m *WebhookModel) UpdateDeliveryResult(ctx context.Context, id uint64, fields map[string]any) error {
	return m.tx(ctx).Model(&WebhookDeliveryEntity{}).Where("id = ?", id).Updates(fields).Error
}

// ClaimDelivery 认领一次投递：把 pending（或租约已过期的 delivering）原子地推进到 delivering。
//
// **为什么必须有这一步**（本批修掉的真实窄窗口）：此前的守卫只有落定时的
// `WHERE status='pending'`，它保证「终态只写一次」，但**拦不住两次出站请求** ——
// 两个 worker（asynq 重试与人工重投撞在一起、或多实例各拿一条同 id 任务）可以同时
// 读到 pending、同时把签名请求发出去，外部系统收到两份一样的事实，而代码只在事后
// 留一条「并发重复投递」的日志。投递是要花时间的（出站 HTTP，客户端超时 15s），
// 「还没投」与「正在投」必须分成两个状态，认领才是原子闸门。
//
// 认领是**条件更新**，不是「读出来判断再写回去」：WHERE 里同时容纳三种可认领情形 ——
//
//	· status = pending：正常入队；
//	· status = delivering 且 update_time < at-lease：上一个认领者已超过租约，
//	  按「worker 崩溃 / 卡死」处理并允许抢占 —— 这就是租约的全部作用；
//	· 其余（终态，或 delivering 且租约未过期）→ 0 行，调用方必须**放弃本次投递**。
//
// 租约只能把重复投递的窗口从「并发读」收紧到「真的卡死了 lease 那么久」，不能彻底消除：
// 被抢占的 worker 可能仍在网络上，我们无法撤回一个已经发出去的请求 —— 所以抢占要留痕
// （见 ReplayPendingDeliveries 的报告与日志），而不是假装它没发生。
//
// 受影响行数 0 **刻意不区分**「日志不存在 / 已被别人认领 / 已落定」：三者对调用方的含义
// 完全一样（这次调用不该再投）。要区分只能额外读一次，而那次读的结论立刻就会过期。
func (m *WebhookModel) ClaimDelivery(ctx context.Context, id uint64, lease time.Duration, at time.Time) (bool, error) {
	at = at.Truncate(time.Microsecond)
	// 先截到微秒再比较、并用同一个值写库：列是 timestamptz(6)，Go 的 time 带纳秒，
	// 直接拿纳秒值去比会被 PG 的舍入结果判成「晚了一点点」—— 5 分钟的租约不在乎，
	// 但「写进去的值 == 用来判定的值」这条不变量值得保住。
	res := m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND (status = ? OR (status = ? AND update_time < ?))",
			id, webhookenums.DeliveryStatusPending,
			webhookenums.DeliveryStatusDelivering, at.Add(-lease)).
		Updates(map[string]any{
			"status":      webhookenums.DeliveryStatusDelivering,
			"update_time": at,
		})
	return res.RowsAffected > 0, res.Error
}

// ReleaseDeliveryClaim 把「认领了但没能投出去」的行退回 pending（只影响仍是 delivering 的行）。
//
// 唯一调用点是 DeliverDelivery 里「认领成功、紧接着连投递日志都读不出来」那条路（同一次
// UPDATE 之后又读同一行，属于不可能但必须收口的异常）。没有它，那一行会停在 delivering
// 直到租约过期 —— 队列的即时重试会因为「租约未过期」被拒，白白等一整个 lease。
//
// 不动 attempts：真的一次投递都没发生，把它算进「试过几次」会让排障时高估。
func (m *WebhookModel) ReleaseDeliveryClaim(ctx context.Context, id uint64, at time.Time) error {
	return m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND status = ?", id, webhookenums.DeliveryStatusDelivering).
		Updates(map[string]any{
			"status":      webhookenums.DeliveryStatusPending,
			"update_time": at.Truncate(time.Microsecond),
		}).Error
}

// MarkDeliveryResult 原子地把**已认领的**投递落定为终态（delivered / failed）并自增 attempts。
//
// 为什么是「条件更新」而不是「读出来 → 算 attempts+1 → 写回去」：
// 后者在并发（asynq 重试与人工重投撞在一起、或多实例各拿一条同 id 任务）下会丢更新 ——
// attempts 是排障时唯一的「这条到底试过几次」证据，读-改-写会把它算少。
//
// WHERE 里的 status=delivering 是**两道守卫合一**：
//
//	· 终态只写一次 —— 第一个落定的调用把状态翻成终态，第二个拿到 0 行；
//	· 没被认领过的行写不进去 —— 于是「先认领、再投递、最后落定」这条顺序不再只是
//	  调用方的自觉，而是数据层的既成事实（未经 ClaimDelivery 的行不可能是 delivering）。
//
// 返回 affected=false 即「有人先处理过了」（并发下的正常现象），调用方据此留痕，
// 而不是把它当成静默成功。
//
// 单条 UPDATE 而不是拆成 mark-delivered / mark-failed 两个方法：投递结果只在一处落定，
// 不可能出现「先写 failed 再写 delivered」这类自相矛盾的操作序列。
func (m *WebhookModel) MarkDeliveryResult(ctx context.Context, id uint64, status string, responseStatus int, lastErr string, at time.Time) (bool, error) {
	res := m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND status = ?", id, webhookenums.DeliveryStatusDelivering).
		Updates(map[string]any{
			"status":          status,
			"attempts":        gorm.Expr("attempts + 1"),
			"response_status": responseStatus,
			"last_error":      lastErr,
			"update_time":     at.Truncate(time.Microsecond),
		})
	return res.RowsAffected > 0, res.Error
}

// MarkDeliveryRetryable 原子地把 failed 投递改回 pending（人工重投的入口动作）。
//
// attempts 不清零：它是「这条投递一共试过几次」的历史，清零会让排障看不出它失败过。
// last_error 清空：留着会让「pending 却带着 last_error」看起来像状态不一致。
// WHERE 里带 status='failed' 是并发防护与语义守卫合一 —— 只有确实处于 failed 的
// 那一条会被改回 pending，受影响行数 0 表示调用方的前置判断已经过期。
//
// **delivering 不在可重投之列**：那条正在被某个 worker 投递（或刚被认领），
// 把它改回 pending 会让队列再派一个 worker 出去 —— 正是认领态要消除的重复投递。
// 要救「认证领者卡死」的那一条，走租约过期后的抢占（ClaimDelivery + 重放）。
func (m *WebhookModel) MarkDeliveryRetryable(ctx context.Context, id uint64, at time.Time) (bool, error) {
	res := m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND status = ?", id, webhookenums.DeliveryStatusFailed).
		Updates(map[string]any{
			"status":      webhookenums.DeliveryStatusPending,
			"last_error":  "",
			"update_time": at.Truncate(time.Microsecond),
		})
	return res.RowsAffected > 0, res.Error
}

// ListStaleDeliveries 列出「该重投的」投递，两条判据合在一个查询里（按 create_time 升序）：
//
//  1. pending 且 create_time 早于 pendingBefore：入队丢了 / 队列压根没启用的那类
//     （真源是这一行，队列只是加速器）；
//  2. delivering 且 update_time 早于 deliveringBefore：认领者已经超过租约，
//     按 worker 崩溃 / 卡死处理。这一条正是「认证领后就再没人管」的收口 ——
//     没有它，认领态会把原来「卡在 pending」的洞变成「卡在 delivering」的洞。
//
// 两个阈值分开传：pending 看 create_time（什么时候创建），delivering 看 update_time
// （什么时候被认领）—— 混用同一个阈值会把「刚创建的 pending」和「刚认领的 delivering」
// 判成同一类，前者会被立刻重投（重复投递），后者要等很久才被救（卡死更久）。
func (m *WebhookModel) ListStaleDeliveries(ctx context.Context, pendingBefore, deliveringBefore time.Time, limit int) (list []*WebhookDeliveryEntity, err error) {
	err = m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("(status = ? AND create_time < ?) OR (status = ? AND update_time < ?)",
			webhookenums.DeliveryStatusPending, pendingBefore,
			webhookenums.DeliveryStatusDelivering, deliveringBefore).
		Order("create_time ASC, id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// CountDeliveriesByStatus 按状态计数（重放报告里的规模数字：pending 与 delivering 各数一次 ——
// 「还没投」与「正在投」是两种完全不同的积压，压成一个数字会让排障看不出是队列没启还是 worker 卡死）。
func (m *WebhookModel) CountDeliveriesByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := m.tx(ctx).Model(&WebhookDeliveryEntity{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

// deliveryFilter 投递日志的查询条件（零值即不筛选）。
//
// 放在 model 而不是 dto：它是持久化层的 WHERE 形状，不是对外协议 ——
// dto 改字段名不该牵动这里，反之亦然。
type deliveryFilter struct {
	EndpointID uint64
	EventType  string
	Status     string
}

// apply 把非零条件挂到查询上。
func (f deliveryFilter) apply(q *gorm.DB) *gorm.DB {
	if f.EndpointID != 0 {
		q = q.Where("endpoint_id = ?", f.EndpointID)
	}
	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	return q
}

// ListDeliveries 按条件分页列出投递日志，最新的在前。
//
// 排序加 id 兜底：一次 DispatchEvent 产生的多条投递 create_time 是同一个 now()，
// 只按时间排序在分页边界上会重复或漏行。
func (m *WebhookModel) ListDeliveries(ctx context.Context, endpointID uint64, eventType, status string, offset, limit int) (list []*WebhookDeliveryEntity, err error) {
	f := deliveryFilter{EndpointID: endpointID, EventType: eventType, Status: status}
	err = f.apply(m.tx(ctx).Model(&WebhookDeliveryEntity{})).
		Order("create_time DESC, id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, err
}

// CountDeliveries 按同一条件计数（分页用；条件口径必须与 ListDeliveries 一致）。
func (m *WebhookModel) CountDeliveries(ctx context.Context, endpointID uint64, eventType, status string) (n int64, err error) {
	f := deliveryFilter{EndpointID: endpointID, EventType: eventType, Status: status}
	err = f.apply(m.tx(ctx).Model(&WebhookDeliveryEntity{})).Count(&n).Error
	return n, err
}
