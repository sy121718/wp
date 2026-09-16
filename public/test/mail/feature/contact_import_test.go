// Package feature mail 模块 feature 测试（真实 PostgreSQL + 生产 DDL，issue #37）。
//
// 本文件盯四件事，其中第一件是**项目首次使用 PG 数组类型**，必须先证明它真的能用：
//
//  1. `[]string` ↔ `text[]` 往返，以及 `tags @> ?::text[]` 筛选（GORM 经 pgx 的数组映射）；
//  2. 导入的逐行校验与**逐行报错**（一行坏数据不毁整批）；
//  3. 批内去重与抑制名单拦截；
//  4. 同意状态：没声明同意的一律 pending（不可发营销），声明了才 subscribed 并留痕。
package feature

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"

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

// TestContactTagsArrayRoundTrip 验证 text[] 在 GORM + pgx 下的读写与包含查询。
//
// 这是项目第一次用 PG 数组；若映射有问题（比如写成 NULL、或 @> 参数类型不匹配），
// 后面所有按标签筛人群的功能都会静默失效，所以单独一条测试把它钉住。
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

	// 包含查询：同时含 vip 与 华南
	list, _, err := f.m.ListContacts(ctx, mailmodel.ContactFilter{Tags: []string{"vip", "华南"}})
	if err != nil {
		t.Fatalf("按标签筛选失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("@> 双标签应命中 1 条，实际 %d", len(list))
	}

	// 不存在的标签组合不应命中
	list, _, err = f.m.ListContacts(ctx, mailmodel.ContactFilter{Tags: []string{"vip", "华北"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("不匹配的标签组合不该命中，实际 %d", len(list))
	}

	// 无标签的联系人（空数组）要能正常写入 —— 列是 NOT NULL DEFAULT '{}'，gorm 不能写 NULL
	if err = f.m.CreateContact(ctx, &mailmodel.MailContactEntity{
		Email:  "plain@example.com",
		Source: mailmodel.ContactSourceManual,
		Status: mailmodel.ContactStatusPending,
		Tags:   nil,
	}); err != nil {
		t.Fatalf("无标签联系人写入失败（Tags 应为空数组而不是 NULL）: %v", err)
	}
}

// TestImportPlainTextReportsBadRows 纯文本导入：坏行逐行报告，好行照常入库。
func TestImportPlainTextReportsBadRows(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	content := "a@example.com\nnot-an-email\n# 注释行\n\nB@Example.COM\nb@example.com\n"
	res, err := f.svc.ImportContacts(ctx, &maildto.ImportContactsReq{Content: []byte(content)})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	// 有效行：a@example.com / B@Example.COM（域名小写化）；b@example.com 与它批内去重。
	if len(res.Errors) != 1 || res.Errors[0].Email != "not-an-email" {
		t.Fatalf("应恰好报 1 行非法，实际 %+v", res.Errors)
	}
	if res.Imported != 2 {
		t.Fatalf("应导入 2 条（第三条与小写后的第二条重复），实际 %d", res.Imported)
	}
	if res.Skipped != 1 {
		t.Fatalf("应有 1 条批内重复被跳过，实际 %d", res.Skipped)
	}

	// 没声明同意 → 一律 pending（不可发营销）
	got, err := f.m.GetContactByEmail(ctx, "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != mailmodel.ContactStatusPending {
		t.Fatalf("未声明同意的导入必须是 pending，实际 %s", got.Status)
	}
	if got.SubscribedAt != nil {
		t.Fatal("pending 不该有 subscribed_at")
	}
}

// TestImportCSVWithHeaderAndConsent CSV 表头映射 + 同意声明留痕。
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
	// 行内标签 + 默认标签合并
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

// TestImportSkipsSuppressed 抑制名单里的地址不进联系人表。
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

// TestImportLargeListScaled 1000 行规模导入：验证批量 upsert 与数组映射在量级下正常。
func TestImportLargeListScaled(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	var b strings.Builder
	const n = 1000
	for i := 0; i < n; i++ {
		// 与用户给的测试名单同形：test{N}{2字母}@clker.cn
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
	// 没声明同意 → 全部 pending → 可发营销的人数是 0（这正是安全默认值）
	if total != 0 {
		t.Fatalf("未声明同意时不该有人可发营销，实际 %d", total)
	}

	// 按标签筛：全部 1003 人都在「规模测试」标签下
	list, _, err := f.m.ListContacts(ctx, mailmodel.ContactFilter{Tags: []string{"规模测试"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("按标签分页取 5 条，实际 %d", len(list))
	}
}
