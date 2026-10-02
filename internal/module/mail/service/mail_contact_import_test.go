package mailservice

// mail_contact_import_test.go — 导入触发链里的两个**纯逻辑**断言（不碰数据库）。
//
// 为什么这两段值得单独测：它们错了都不会报错，只会静默改变触发次数 ——
// 「差集」算错会让重复导入反复触发 tag_added（重复发信），
// 「失败行」判错会让没写进库的行也触发（按不存在的事实发信）。

import (
	"reflect"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
)

// TestDiffTagsOnlyNewOnes 只有本次新增的标签进入差集。
func TestDiffTagsOnlyNewOnes(t *testing.T) {
	cases := []struct {
		name     string
		old      []string
		incoming []string
		want     []string
	}{
		{"全部是新的", nil, []string{"vip", "pro"}, []string{"vip", "pro"}},
		{"全部已存在", []string{"vip"}, []string{"vip"}, nil},
		{"部分新增（保序）", []string{"vip"}, []string{"vip", "pro"}, []string{"pro"}},
		{"旧值带空白也算已存在", []string{" vip "}, []string{"vip"}, nil},
		{"入参里的空白与空串被丢弃", []string{"vip"}, []string{"", "  ", "pro"}, []string{"pro"}},
		{"同一批里重复的标签只算一次", nil, []string{"vip", "vip"}, []string{"vip"}},
		{"缺少标签的导入不产生触发", []string{"vip"}, nil, nil},
		// 比较是精确匹配（大小写敏感）：与按标签筛人群的口径一致，
		// 不是「等价于新增」——VIP 与 vip 在库里就是两个标签。
		{"大小写不同视为不同标签", []string{"vip"}, []string{"VIP"}, []string{"VIP"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diffTags(tc.old, tc.incoming); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("diffTags(%v, %v) = %v，期望 %v", tc.old, tc.incoming, got, tc.want)
			}
		})
	}
}

// TestFailedImportEmailsNormalizes 失败行按小写邮箱归一，供触发侧跳过。
func TestFailedImportEmailsNormalizes(t *testing.T) {
	res := &maildto.ImportContactsResp{Errors: []maildto.ImportRowError{
		{Email: "Bad@Example.com", Reason: "更新失败: 联系人已不存在"},
		{Line: 3, Email: "  spaced@Example.com ", Reason: "非法邮箱"},
		{Line: 9, Reason: "CSV 解析失败: 无表头"}, // 没有 email：不该产生空键
	}}
	got := failedImportEmails(res)
	want := map[string]bool{"bad@example.com": true, "spaced@example.com": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failedImportEmails = %v，期望 %v", got, want)
	}
	if got[""] {
		t.Fatal("空邮箱不该进入失败集合（否则会误伤所有行）")
	}
}
