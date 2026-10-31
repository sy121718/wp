package mailservice

import (
	"reflect"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
)

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

func TestFailedImportEmailsNormalizes(t *testing.T) {
	res := &maildto.ImportContactsResp{Errors: []maildto.ImportRowError{
		{Email: "Bad@Example.com", Reason: "更新失败: 联系人已不存在"},
		{Line: 3, Email: "  spaced@Example.com ", Reason: "非法邮箱"},
		{Line: 9, Reason: "CSV 解析失败: 无表头"},
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
