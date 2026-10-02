package feature

// contact_import_batching_test.go — 联系人导入的写入形态探针（审计 DB-006）。
//
// 审计关注「批量导入的写入方式」。把结论变成可断言的事实需要两个量：
//   1. 一次导入实际发出多少条 SQL —— 逐条写入的 N 条 vs 集合写入的常数条；
//   2. 大名单导入的墙钟耗时（量级参考，不作硬断言，避免机器差异导致 flaky）。
//
// 这里用 gorm 的 Logger 计数：模型层换实现（逐条 / CreateInBatches / COPY 到临时表）
// 都会在语句数上留下可直接观察的差异。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"

	"go_wp/public/test/support"
)

// sqlCounter 统计实际执行的语句数，并留几条样本供报告使用。
type sqlCounter struct {
	mu     sync.Mutex
	count  int
	sample []string
}

func (c *sqlCounter) LogMode(logger.LogLevel) logger.Interface      { return c }
func (c *sqlCounter) Info(context.Context, string, ...interface{})  {}
func (c *sqlCounter) Warn(context.Context, string, ...interface{})  {}
func (c *sqlCounter) Error(context.Context, string, ...interface{}) {}
func (c *sqlCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	if len(c.sample) < 4 {
		c.sample = append(c.sample, sql)
	}
}

func (c *sqlCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count = 0
	c.sample = nil
}

func (c *sqlCounter) snapshot() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count, append([]string(nil), c.sample...)
}

// importFixture 建库 + 迁移，并把 db 挂上语句计数器。
func importFixture(t *testing.T, counter *sqlCounter) (*mailservice.Service, *mailmodel.MailModel) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	db = db.Session(&gorm.Session{Logger: counter})
	m := mailmodel.NewMailModel(db)
	return mailservice.NewService(m), m
}

// TestImportWriteStatementProfile 导入的写入形态：全新新增 vs 命中已存在。
func TestImportWriteStatementProfile(t *testing.T) {
	counter := &sqlCounter{}
	svc, m := importFixture(t, counter)
	if svc == nil {
		return
	}
	ctx := context.Background()

	const n = 2000
	addrs := make([]string, 0, n)
	var b strings.Builder
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("profile%d@example.com", i)
		addrs = append(addrs, addr)
		b.WriteString(addr)
		b.WriteString("\n")
	}
	content := []byte(b.String())

	// 第一遍：全部是新增。
	counter.reset()
	t0 := time.Now()
	first, err := svc.ImportContacts(ctx, &maildto.ImportContactsReq{Content: content, UpdateExisting: true})
	if err != nil {
		t.Fatalf("首次导入失败: %v", err)
	}
	firstElapsed := time.Since(t0)
	firstStmts, _ := counter.snapshot()
	if first.Imported != n {
		t.Fatalf("首次应导入 %d 条，实际 %d", n, first.Imported)
	}

	// 第二遍：同一批全部命中已存在（更新分支）。
	counter.reset()
	t0 = time.Now()
	second, err := svc.ImportContacts(ctx, &maildto.ImportContactsReq{Content: content, UpdateExisting: true})
	if err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	secondElapsed := time.Since(t0)
	secondStmts, sample := counter.snapshot()
	if second.Updated != n {
		t.Fatalf("二次应更新 %d 条，实际 %d（错误 %+v）", n, second.Updated, second.Errors)
	}

	// 防退化护栏：语句数必须远小于行数。退回逐条 UPDATE 会立刻变成 N 条，
	// 退回逐条 INSERT 同理（CreateInBatches 是 500 行一条语句）。
	if secondStmts > 10 {
		t.Errorf("更新路径应走集合式写回，实际发出 %d 条语句（%d 行）—— 退回逐条了？", secondStmts, n)
	}
	if firstStmts > 20 {
		t.Errorf("新增路径应走 CreateInBatches，实际发出 %d 条语句（%d 行）", firstStmts, n)
	}

	// 对照：改动前的形态（逐条 UPDATE 同一批数据）在相同条件下要花多少。
	existing, err := m.ExistingContactIDs(ctx, addrs)
	if err != nil {
		t.Fatal(err)
	}
	counter.reset()
	t0 = time.Now()
	for _, addr := range addrs {
		fields := map[string]any{"source": mailmodel.ContactSourceImport, "update_time": time.Now()}
		if uerr := m.UpdateContactFields(ctx, existing[strings.ToLower(addr)], fields); uerr != nil {
			t.Fatal(uerr)
		}
	}
	legacyElapsed := time.Since(t0)
	legacyStmts, _ := counter.snapshot()
	t.Logf("对照（逐条 UPDATE，改动前形态）: %d 行, 语句数=%d, 耗时=%v", n, legacyStmts, legacyElapsed)
	t.Logf("新增路径: %d 行, 语句数=%d, 耗时=%v", n, firstStmts, firstElapsed)
	t.Logf("更新路径: %d 行, 语句数=%d, 耗时=%v", n, secondStmts, secondElapsed)
	for i, s := range sample {
		t.Logf("样本 %d: %.160s", i, s)
	}
}

// TestImportUpdateKeepsConsentState 「更新已存在」分支只动非同意字段：
//
// 退订过的人重新出现在名单里，不能被一次导入悄悄变回可营销 —— 这是合规口径，
// 也是批量写回必须与逐条路径逐字保持一致的地方（DB-006 改的是写法，不是语义）。
func TestImportUpdateKeepsConsentState(t *testing.T) {
	counter := &sqlCounter{}
	svc, m := importFixture(t, counter)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 带表头的 CSV 才会映射 name 列（无表头时第一列当邮箱）。
	if _, err := svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: []byte("email,name\nconsent@example.com,旧名\n"),
	}); err != nil {
		t.Fatal(err)
	}
	// 模拟退订：追踪端点走的就是这条路径。
	if err := m.UpdateContactStatusByEmail(ctx, "consent@example.com", mailmodel.ContactStatusUnsubscribed, time.Now()); err != nil {
		t.Fatal(err)
	}

	// 再次导入同一地址，且本次声明了同意 —— 也不得把退订状态改写回去。
	res, err := svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:         []byte("email,name\nconsent@example.com,新名\n"),
		UpdateExisting:  true,
		ConsentDeclared: true,
		ConsentSource:   "第二次导入（不应覆盖已退订状态）",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 1 {
		t.Fatalf("应更新 1 条，实际 %+v", res)
	}

	got, err := m.GetContactByEmail(ctx, "consent@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != mailmodel.ContactStatusUnsubscribed {
		t.Fatalf("导入更新不得改写同意状态：期望 unsubscribed，实际 %s", got.Status)
	}
	if got.SubscribedAt != nil {
		t.Fatal("退订的人不该有 subscribed_at")
	}
	if got.ConsentSource != nil {
		t.Fatalf("同意来源不该被导入写入: %v", *got.ConsentSource)
	}
	// 非同意字段照常更新，证明批量路径确实写进去了。
	if got.Name == nil || *got.Name != "新名" {
		t.Fatalf("非同意字段（姓名）应被更新，实际 %v", got.Name)
	}
}

// TestImportTagDiffReadsOldTagsInOneQuery 有 tag_added 流程时，标签差集的「旧值读取」
// 必须是**一次批量读**，不能是每个联系人一次主键读。
//
// 为什么单开一条：`needTagDiff` 的粗判（有没有启用中的 tag_added 流程）让零配置站点
// 根本不走到这段 —— 导入语句数恒为常数，所以既有的 TestImportWriteStatementProfile
// 覆盖不到它：把批量读退回逐条读时，那条护栏照样绿。
// N 取 300：逐条读会立刻变成 300 条，批量读是常数条（阈值 20）。
func TestImportTagDiffReadsOldTagsInOneQuery(t *testing.T) {
	counter := &sqlCounter{}
	svc, _ := importFixture(t, counter)
	ctx := context.Background()

	// 「被打上 vip 标签」的启用流程 —— 它是 needTagDiff 为真的唯一条件。
	def := map[string]any{"entry": "n1", "nodes": []any{
		newNode("n1", "trigger", nil, "n2"),
		newNode("n2", "end", nil, ""),
	}}
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name: "打上 vip 标签跟进", TriggerType: mailmodel.TriggerTagAdded,
		Definition: raw, TriggerParams: map[string]any{"tag": "vip"},
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	if err = svc.SetAutomationStatus(ctx, &maildto.SetAutomationStatusReq{
		ID: item.ID, Status: mailmodel.AutomationStatusActive,
	}); err != nil {
		t.Fatalf("启用流程失败: %v", err)
	}

	const n = 300
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "tagdiff%d@example.com\n", i)
	}
	content := []byte(sb.String())

	// 第一遍：全部新增（新联系人的旧标签集合为空，无需读差集）。
	if _, err = svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: content, DefaultTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("首轮导入失败: %v", err)
	}

	// 第二遍：同一批全部命中已存在 → 走 needTagDiff 的差集读；标签没有新增，
	// 所以除了那一次批量读，不该再为每行付出任何读取。
	counter.reset()
	res, err := svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: content, DefaultTags: []string{"vip"}, UpdateExisting: true,
	})
	if err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	if res.Updated != n {
		t.Fatalf("二次应更新 %d 条，实际 %d（错误 %+v）", n, res.Updated, res.Errors)
	}
	stmts, sample := counter.snapshot()
	if stmts > 20 {
		t.Errorf("差集读退回逐条了：%d 行发出 %d 条语句（阈值 20）—— 旧标签必须一次批量读", n, stmts)
		for i, s := range sample {
			t.Logf("样本 %d: %.160s", i, s)
		}
	}
	t.Logf("有 tag_added 流程时二次导入 %d 行：语句数=%d", n, stmts)
}
