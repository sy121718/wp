package aimodel_test

import (
	"sync"
	"testing"

	"gorm.io/gorm"

	aimodel "go_wp/internal/module/ai/model"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 本文件证明 SessionUsageOf 的四段聚合跑在同一份快照里。
//
// 手法：在一次 SessionUsageOf 调用内部，用 GORM 查询 callback 在**第一次聚合完成之后**
// 触发一次写入（走独立连接、独立事务并提交），然后让剩下的聚合继续跑。
// 如果四段不是同一快照，第二次及之后的聚合就会看到这批新行，数字就变了；
// 只有真正跑在 REPEATABLE READ 事务里，整次调用的结果才会与并发写入之前一致。
//
// 为什么放在 model 同包：这条断言要看的正是 sessionUsageTx 与它外面那层事务的关系，
// 用生产入口 SessionUsageOf 直接观察即可 —— 不需要、也不该为测试在生产代码上加
// 「在给定 tx 里跑一遍」这类入口（那会让测试跑的前提与生产真实路径不一致）。
func TestSessionUsageOfStaysOnOneSnapshotAcrossConcurrentWrite(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产种子失败：%v", err)
	}
	m := aimodel.NewSessionModel(db)
	ctx := t.Context()

	const sessionID int64 = 90001
	if err := db.Exec(
		`INSERT INTO ai_session (id, session_key, title, status, next_seq, create_by, update_by)
		 VALUES (?, 'snapshot-key', '快照用例', 1, 10, 7, 7)`, sessionID).Error; err != nil {
		t.Fatalf("准备会话失败：%v", err)
	}
	if err := db.Exec(
		`INSERT INTO ai_event (session_id, seq, kind, content, content_tokens)
		 VALUES (?, 1, 'user', '第一句', 5)`, sessionID).Error; err != nil {
		t.Fatalf("准备事件失败：%v", err)
	}

	// 并发写入只触发一次：callback 里用一个 sync.Once 保证不会在第二段、第三段聚合上
	// 重复插入（否则「两次结果相同」会因为写入时机而变得不可判定）。
	var once sync.Once
	var writeErr error
	const extraEvents = 5
	const extraTokens = 7

	before := countEvents(t, db, sessionID)
	if before != 1 {
		t.Fatalf("准备阶段应只有 1 条事件，实际 %d", before)
	}

	cbName := "test:session_usage_concurrent_write"
	if err := db.Callback().Query().After("gorm:query").Register(cbName, func(tx *gorm.DB) {
		// 一次触发：sync.Once 保证只写一次，后续聚合不会被自己再刷一遍。
		once.Do(func() {
			// 独立连接 / 独立事务：不能复用调用方的 tx，否则写与读同事务、永远看不见。
			rows := make([]map[string]any, 0, extraEvents)
			for i := 0; i < extraEvents; i++ {
				rows = append(rows, map[string]any{
					"session_id":     sessionID,
					"seq":            int64(100 + i),
					"kind":           "user",
					"surface_op":     "append",
					"content":        "并发写入",
					"content_tokens": int64(extraTokens),
				})
			}
			writeErr = db.Table("ai_event").Create(&rows).Error
		})
	}); err != nil {
		t.Fatalf("注册回调失败：%v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(cbName)
	})

	usage, err := m.SessionUsageOf(ctx, aimodel.SessionQuery{Status: -1})
	if err != nil {
		t.Fatalf("读用量失败：%v", err)
	}
	// 先确认并发写入真的发生了 —— 否则这个用例会因为「什么都没写」而假通过。
	if writeErr != nil {
		t.Fatalf("并发写入失败：%v", writeErr)
	}
	if after := countEvents(t, db, sessionID); after != before+extraEvents {
		t.Fatalf("并发写入应已提交：%d -> %d（想要 +%d）", before, after, extraEvents)
	}

	// 事务外读到的总量 = 旧的一行 + 并发写入的五行。
	wantEvents := before
	wantTokens := int64(5)
	if usage.Events != wantEvents {
		t.Fatalf("并发写入落在调用期间，聚合结果却应是快照里的旧值：events=%d, 想要 %d", usage.Events, wantEvents)
	}
	if usage.Tokens != wantTokens {
		t.Fatalf("token 同样应来自同一份快照：tokens=%d, 想要 %d", usage.Tokens, wantTokens)
	}

	// 事务结束后重新读，必须看见新行 —— 证明上面的「一致」是快照隔离，而不是读路径漏了新数据。
	fresh, err := m.SessionUsageOf(ctx, aimodel.SessionQuery{Status: -1})
	if err != nil {
		t.Fatalf("二次读用量失败：%v", err)
	}
	if fresh.Events != before+extraEvents || fresh.Tokens != int64(5+extraEvents*extraTokens) {
		t.Fatalf("新调用应看见并发写入后的数据：%+v", fresh)
	}
}

func countEvents(t *testing.T, db *gorm.DB, sessionID int64) int64 {
	t.Helper()
	var n int64
	if err := db.Table("ai_event").Where("session_id = ?", sessionID).Count(&n).Error; err != nil {
		t.Fatalf("数事件失败：%v", err)
	}
	return n
}
