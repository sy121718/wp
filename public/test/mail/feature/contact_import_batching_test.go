package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
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

func importFixture(t *testing.T, counter *sqlCounter) (*mailservice.Service, *mailmodel.MailModel) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	db = db.Session(&gorm.Session{Logger: counter})
	m := mailmodel.NewMailModel(db)
	return mailservice.NewService(m), m
}

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

	if secondStmts > 10 {
		t.Errorf("更新路径应走集合式写回，实际发出 %d 条语句（%d 行）—— 退回逐条了？", secondStmts, n)
	}
	if firstStmts > 20 {
		t.Errorf("新增路径应走 CreateInBatches，实际发出 %d 条语句（%d 行）", firstStmts, n)
	}

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

func TestImportUpdateKeepsConsentState(t *testing.T) {
	counter := &sqlCounter{}
	svc, m := importFixture(t, counter)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if _, err := svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: []byte("email,name\nconsent@example.com,旧名\n"),
	}); err != nil {
		t.Fatal(err)
	}

	if err := m.UpdateContactStatusByEmail(ctx, "consent@example.com", mailmodel.ContactStatusUnsubscribed, time.Now()); err != nil {
		t.Fatal(err)
	}

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

	if got.Name == nil || *got.Name != "新名" {
		t.Fatalf("非同意字段（姓名）应被更新，实际 %v", got.Name)
	}
}

func TestImportTagDiffReadsOldTagsInOneQuery(t *testing.T) {
	counter := &sqlCounter{}
	svc, _ := importFixture(t, counter)
	ctx := context.Background()

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

	if _, err = svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: content, DefaultTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("首轮导入失败: %v", err)
	}

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
