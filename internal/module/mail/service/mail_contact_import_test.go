package mailservice

import (
	"errors"
	"reflect"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
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

// TestImportRowBlockedCarriesRowsAndReason 守住导入的非法行整批拒绝事实：
// 行号进 id、邮箱进 name、原因 key 原样保留，主消息只有公共一句。
func TestImportRowBlockedCarriesRowsAndReason(t *testing.T) {
	err := importRowBlocked([]maildto.ImportRowError{
		{Email: "Bad@Example.com", Reason: mailenums.ErrContactNotFound},
		{Line: 3, Email: "  spaced@Example.com ", Reason: mailenums.ErrEmailInvalid},
	})
	var blocked *mailBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("应产出整批拒绝事实，实际 %T", err)
	}
	if blocked.Error() != mailenums.ErrBatchReject {
		t.Fatalf("主消息 key = %q，want %q", blocked.Error(), mailenums.ErrBatchReject)
	}
	items := blocked.BlockedItems()
	if len(items) != 2 {
		t.Fatalf("拒绝项 = %d，want 2", len(items))
	}
	if items[0].ID != "" || items[0].Name != "Bad@Example.com" || items[0].ReasonKey != mailenums.ErrContactNotFound {
		t.Fatalf("第 1 项 = %+v", items[0])
	}
	if items[1].ID != "3" || items[1].Name != "spaced@Example.com" || items[1].ReasonKey != mailenums.ErrEmailInvalid {
		t.Fatalf("第 2 项 = %+v", items[1])
	}
}
