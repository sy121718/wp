package aimodel

// ai_tool_idempotency_model.go — 写工具的幂等台账（迁移 569）。
//
// 只做两件事：按 (tool_name, idem_key) 取上次结果、按同键记下这次结果。
// 不带任何「要不要执行」的判断 —— 那是 mcp 层的地基（internal/mcp/write.go）的事，
// 放这里会让「写工具的三件套」散在两个模块里。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// tableNameAIToolIdempotency 表名。
const tableNameAIToolIdempotency = "ai_tool_idempotency"

// idempotencyWindow 幂等键的有效期。
//
// 只挡「短时间内的重复提交」：更久的重试要么是人为操作（那本来就该当成一次新意图），
// 要么早就被别的手段拦住了。窗口写在这里而不是 SQL 里，是为了让它可被单独断言。
const idempotencyWindow = 24 * time.Hour

// ToolIdempotencyRow 一行幂等记录。
type ToolIdempotencyRow struct {
	ToolName   string    `gorm:"column:tool_name"`
	IdemKey    string    `gorm:"column:idem_key"`
	ResultText string    `gorm:"column:result_text"`
	CreateTime time.Time `gorm:"column:create_time"`
}

// ToolIdempotencyModel 负责 ai_tool_idempotency 一张表。
type ToolIdempotencyModel struct{ db *gorm.DB }

// NewToolIdempotencyModel 构造。
func NewToolIdempotencyModel(db *gorm.DB) *ToolIdempotencyModel {
	return &ToolIdempotencyModel{db: db}
}

// Lookup 取同一次意图上次成功的结果；没有（或已过期）时 ok=false。
//
// 过期**不删行**：这是个只读路径，顺手删除会让一次查询变成写事务，
// 而它会在工具调用的热路径上被调用。历史行由人工定期清。
func (m *ToolIdempotencyModel) Lookup(ctx context.Context, tool, key string) (row ToolIdempotencyRow, ok bool, err error) {
	err = m.db.WithContext(ctx).Raw(
		`SELECT tool_name, idem_key, result_text, create_time
		   FROM ai_tool_idempotency
		  WHERE tool_name = ? AND idem_key = ? AND create_time > now() - ?::interval
		  LIMIT 1`,
		tool, key, idempotencyWindow.String(),
	).Scan(&row).Error
	if err != nil {
		return ToolIdempotencyRow{}, false, err
	}
	if row.ToolName == "" {
		return ToolIdempotencyRow{}, false, nil
	}
	return row, true, nil
}

// Save 记下这次成功的结果。
//
// **ON CONFLICT DO NOTHING**：并发下两个相同键的调用可能同时走到这里，
// 先到的写、后到的忽略 —— 报错会让后到的那次变成「失败」，而它的业务写入
// 其实已经完成了（幂等记录没落上不等于这次没执行）。
//
// 只存 result_text，不存 result_data：写工具的返回值是给人读的一句话；
// 带结构化数据的工具不该是写工具（那类数据的消费者是渲染通道，见 ai_session_render.go）。
func (m *ToolIdempotencyModel) Save(ctx context.Context, tool, key, resultText string) error {
	return m.db.WithContext(ctx).Exec(
		`INSERT INTO ai_tool_idempotency (tool_name, idem_key, result_text)
		 VALUES (?, ?, ?)
		 ON CONFLICT (tool_name, idem_key) DO NOTHING`,
		tool, key, resultText,
	).Error
}
