package feature

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/pkg/facing"

	"go_wp/public/test/support"
)

type fixture struct {
	svc *mailservice.Service
	m   *mailmodel.MailModel
	db  *gorm.DB
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	m := mailmodel.NewMailModel(db)
	return &fixture{svc: mailservice.NewService(m), m: m, db: db}
}

func TestContactTagsArrayRoundTrip(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	created := &mailmodel.MailContactEntity{
		Email:  "vip@example.com",
		Source: mailmodel.ContactSourceManual,
		Status: mailmodel.ContactStatusSubscribed,
		Tags:   mailmodel.StringArray{"vip", "华南"},
	}
	if err := f.m.CreateContact(ctx, created); err != nil {
		t.Fatalf("写入带标签的联系人失败: %v", err)
	}
	got, err := f.m.GetContact(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "vip" || got.Tags[1] != "华南" {
		t.Fatalf("标签往返不一致: %#v", got.Tags)
	}

	list, _, err := f.m.ListContacts(ctx, mailmodel.ContactFilter{Tags: []string{"vip", "华南"}})
	if err != nil {
		t.Fatalf("按标签筛选失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("@> 双标签应命中 1 条，实际 %d", len(list))
	}

	list, _, err = f.m.ListContacts(ctx, mailmodel.ContactFilter{Tags: []string{"vip", "华北"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("不匹配的标签组合不该命中，实际 %d", len(list))
	}

	if err = f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
		Email:  "plain@example.com",
		Source: mailmodel.ContactSourceManual,
		Status: mailmodel.ContactStatusPending,
		Tags:   nil,
	}); err != nil {
		t.Fatalf("无标签联系人写入失败（Tags 应为空数组而不是 NULL）: %v", err)
	}
}

func TestImportPlainTextReportsBadRows(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	content := "a@example.com\nnot-an-email\n# 注释行\n\nB@Example.COM\nb@example.com\n"
	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{Content: []byte(content)})
	if err == nil {
		t.Fatalf("含非法行的导入必须整批拒绝，实际成功：%+v", res)
	}
	if res != nil {
		t.Fatalf("整批拒绝的导入不该返回结果，实际 %+v", res)
	}
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("应给结构化拒绝事实，实际 %T", err)
	}
	items := blocked.BlockedItems()
	if len(items) != 1 || items[0].Name != "not-an-email" {
		t.Fatalf("应恰好报 1 行非法，实际 %+v", items)
	}
	// 零提交：两条去重后的合法邮箱（a@example.com 与 B@Example.COM/b@example.com 合并为 b）都不能落库。
	var persisted []string
	if qerr := f.db.Model(&mailmodel.MailContactEntity{}).
		Where("lower(email) IN ?", []string{"a@example.com", "b@example.com"}).
		Pluck("lower(email)", &persisted).Error; qerr != nil {
		t.Fatalf("查询落库联系人失败: %v", qerr)
	}
	if len(persisted) != 0 {
		t.Fatalf("非法行存在时整批回滚，合法邮箱都不该被导入，实际 %v", persisted)
	}
}

func TestImportCSVWithHeaderAndConsent(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	content := "email,name,tags\nc1@example.com,张三,vip;老客\nc2@example.com,李四,\n"
	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:         []byte(content),
		DefaultTags:     []string{"活动导入"},
		ConsentDeclared: true,
		ConsentSource:   "线下活动收集（2026-09 展会）",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 {
		t.Fatalf("应导入 2 条，实际 %d（错误 %+v）", res.Imported, res.Errors)
	}

	c1, err := f.m.GetContactByEmail(ctx, "c1@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if c1.Status != mailmodel.ContactStatusSubscribed {
		t.Fatalf("声明同意后应为 subscribed，实际 %s", c1.Status)
	}
	if c1.SubscribedAt == nil {
		t.Fatal("subscribed 必须有 subscribed_at（合规留痕）")
	}
	if c1.ConsentSource == nil || !strings.Contains(*c1.ConsentSource, "展会") {
		t.Fatalf("同意来源没落库: %v", c1.ConsentSource)
	}
	if c1.Name == nil || *c1.Name != "张三" {
		t.Fatalf("姓名列没映射上: %v", c1.Name)
	}

	want := map[string]bool{"vip": true, "老客": true, "活动导入": true}
	if len(c1.Tags) != 3 {
		t.Fatalf("标签应为 3 个（行内 2 + 默认 1），实际 %#v", c1.Tags)
	}
	for _, tag := range c1.Tags {
		if !want[tag] {
			t.Fatalf("出现意外标签: %s", tag)
		}
	}
}

func TestImportSkipsSuppressed(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	if err := f.m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
		Email: "blocked@example.com", Reason: mailmodel.SuppressionReasonUnsubscribe,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content: []byte("ok@example.com\nblocked@example.com\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Suppressed != 1 || res.Imported != 1 {
		t.Fatalf("应跳过 1 条被抑制、导入 1 条，实际 %+v", res)
	}
	if _, err := f.m.GetContactByEmail(ctx, "blocked@example.com"); err == nil {
		t.Fatal("被抑制的地址不该进联系人表")
	}
}

func TestImportLargeListScaled(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	var b strings.Builder
	const n = 1000
	for i := 0; i < n; i++ {

		fmt.Fprintf(&b, "test%d%s@clker.cn\n", i, string(rune('a'+i%26))+string(rune('a'+(i/26)%26)))
	}
	b.WriteString("kf@clker.cn\nwork@clker.cn\ncs@vapechoiceau.com\n")

	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{
		Content:     []byte(b.String()),
		DefaultTags: []string{"规模测试"},
	})
	if err != nil {
		t.Fatalf("规模导入失败: %v", err)
	}
	if res.Imported != n+3 {
		t.Fatalf("应导入 %d 条，实际 %d（错误数 %d）", n+3, res.Imported, len(res.Errors))
	}

	total, err := f.m.CountSubscribedByTags(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if total != 0 {
		t.Fatalf("未声明同意时不该有人可发营销，实际 %d", total)
	}

	list, _, err := f.m.ListContacts(ctx, mailmodel.ContactFilter{Tags: []string{"规模测试"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("按标签分页取 5 条，实际 %d", len(list))
	}
}
