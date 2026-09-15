// Package masterdatacontract 主数据变更记录模块对外契约（issue #19）。
//
// 边界：本模块只负责「把字段级主数据变更原样记下来 + 按实体查得出来」。
//
//   - 它不认识商品 / 库存表：调用方（product / inventory 模块）把「改前 / 改后」的
//     字段快照递进来，本模块只做 diff、落库、查询；
//   - 与库存流水**职责分离**：数量增减走库存流水（inventory_stock_movements），
//     结构化字段的配置变更走本模块。同一次业务动作可以两边各记一处，但记的东西不同
//     （「库存 +10」 vs 「货源结算价 3.5 → 4.2」），绝不互相替代；
//   - 一旦写入不可改写：表是 append-only（迁移 111 的 BEFORE UPDATE OR DELETE 触发器
//     在数据库层兜底），本模块不提供任何更新 / 删除入口。
//
// 依赖方向：product / inventory → masterdata。本模块不反向依赖任何业务模块。
package masterdatacontract

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"

	masterdatadto "go_wp/internal/module/masterdata/dto"
	"go_wp/pkg/money"

	"gorm.io/gorm"
)

// FieldSnapshot 一次写操作的字段快照：字段名 → 已格式化的取值。
//
// 「已格式化」是刻意的：价格 99.5 与 99.50 是同一次变更（值相等即不写记录），
// 版本元数据（update_time 之类）不在快照里，就不会产生噪声记录。
// 字段名由各模块自己定义（product / product_variant / inventory_source 各有白名单），
// 展示文案由本模块 enums 统一登记。
type FieldSnapshot map[string]string

// ChangeInput 一次主数据写操作产生的变更（不可变 DTO，跨模块传递）。
//
// 语义由 Action 决定：
//
//	create —— Before 为空、After 是该实体创建后的字段快照（old 为空串）；
//	update —— Before / After 都是快照，只写**真正变化**的字段；
//	delete —— Before 是删除前的快照、After 为空（new 为空串）。
//
// 两侧都缺的字段不会产生记录；因此「更新后某字段没变」不会在历史里留下噪声。
type ChangeInput struct {
	// ProjectID 工程（必填；变更记录按工程隔离）。
	ProjectID string
	// EntityType 实体类型（必填，白名单见 enums.EntityTypes）。
	EntityType string
	// EntityID 实体 id（必填）。
	EntityID string
	// EntityLabel 实体展示名快照（商品名 / SKU 编码 / 货源名）；可空。
	EntityLabel string
	// Action create / update / delete（必填，白名单见 enums.Actions）。
	Action string
	// Origin 记录来源路径（product / variant / variant_generate / pricing / receipt / source）。
	Origin string
	// OperatorID 操作人（会话里的登录名；缺失为空串）。
	OperatorID string
	Before     FieldSnapshot
	After      FieldSnapshot
}

// MasterDataService 主数据变更记录契约。
type MasterDataService interface {
	// RecordChanges 追加变更记录（append-only）。
	//
	// 一次调用可以携带多条 ChangeInput（例如删商品时连带它的全部变体），
	// 保持同一次写操作产生的记录在同一批里落库。
	RecordChanges(ctx context.Context, inputs []*ChangeInput) (err error)
	// RecordChangesTx 在外部事务内追加变更记录（与业务写操作同事务，CQ-026）。
	RecordChangesTx(ctx context.Context, tx *gorm.DB, inputs []*ChangeInput) (err error)

	// ListChanges 按条件查字段级变更（时间倒序）。
	ListChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (list []*masterdatadto.ChangeResp, err error)
	// CountChanges 同条件的总条数（分页用）。
	CountChanges(ctx context.Context, req *masterdatadto.ListChangeReq) (n int64, err error)
	// ListEntities 按实体聚合的变更历史清单（验收 4 的入口：先看「哪些实体被改过」）。
	ListEntities(ctx context.Context, req *masterdatadto.ListEntityReq) (list []*masterdatadto.EntityHistoryResp, err error)
	// CountEntities 同条件的实体数（分页用）。
	CountEntities(ctx context.Context, req *masterdatadto.ListEntityReq) (n int64, err error)
	// EntityTimeline 单个实体的完整变更时间线（实体类型 + 实体 id）。
	EntityTimeline(ctx context.Context, req *masterdatadto.EntityTimelineReq) (res *masterdatadto.EntityTimelineResp, err error)
}

// —— 快照格式化助手 ——
//
// 放在契约里是因为「怎么把一个值写成可比较的字符串」是调用方与本模块之间的约定：
// 双方用同一套格式，diff 才不会因为 99 与 99.00 这种表示差异产生假记录。

// FormatPrice 金额（numeric(12,2)）→ 固定两位小数的字符串。
//
// 「固定两位」是**审计口径**而不是展示口径：本表逐字段记 old/new，"99" 与 "99.00"
// 会被判成一次变更，留下「值没变但审计多了一条」的假记录。
//
// 实现收敛到 pkg/money.FormatAudit（审计 CQ-013：此前 masterdata / product /
// runtimefragment / dashboard 各有一份金额格式化，其中两份逐字节相同、两份口径不同）。
func FormatPrice(v float64) string {
	return money.FormatAudit(v)
}

// FormatPricePtr 可空金额 → 字符串（nil / 空指针 → 空串，空串参与 diff 表示「无值」）。
func FormatPricePtr(v *float64) string {
	if v == nil {
		return ""
	}
	return FormatPrice(*v)
}

// FormatBool 布尔 → "true" / "false"（后台展示与 diff 都按这一种写法）。
func FormatBool(v bool) string {
	return strconv.FormatBool(v)
}

// FormatInt 整数 → 十进制字符串。
func FormatInt(v int) string {
	return strconv.Itoa(v)
}

// FormatStringPtr 可空字符串 → 字符串（nil → 空串）。
func FormatStringPtr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

// FormatJSON jsonb → 压缩后的 JSON 文本（语义相同、空白不同的对象不会产生假记录）。
//
// 解析失败时原样回吐字符串：变更记录宁可记下「原样是什么」，也不吞掉数据。
func FormatJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// NewSnapshot 由「字段名, 取值」成对参数构造快照（调用点更短，也少一层 map 字面量）。
//
// 奇数个参数时最后一项被忽略（调用方写错顺序不会 panic，但会少一个字段）。
func NewSnapshot(pairs ...string) FieldSnapshot {
	out := FieldSnapshot{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i]] = pairs[i+1]
	}
	return out
}
