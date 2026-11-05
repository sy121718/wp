package sysconfigenums

import (
	"testing"

	"go_wp/pkg/facing"
)

func TestErrCarriesKeyNotBundledText(t *testing.T) {
	for _, key := range []string{ErrInvalidParam, ErrVersionRequired, ErrGroupNotFound, ErrVersionConflict} {
		err := Err(key)
		if err.Error() != key {
			t.Errorf("Err(%q).Error() = %q，want 原 key", key, err.Error())
		}
		// enums 只声明 key + 状态码：FactOf 走 Source 接口拿到 (key, "")，
		// 所以兜底必须是空串 —— 内嵌任何中文都说明文案又回到了声明处。
		fact, ok := facing.FactOf(err)
		if !ok {
			t.Errorf("%q 没有被判成业务事实", key)
			continue
		}
		if fact.Key != key {
			t.Errorf("Key = %q，want %q", fact.Key, key)
		}
		if fact.Fallback != "" {
			t.Errorf("%q 内嵌了兜底文案 %q —— enums 只声明 key，文案归词库", key, fact.Fallback)
		}
		if !fact.Business {
			t.Errorf("%q 应标记为业务事实", key)
		}
	}

	if err := Err("sysconfig.err.something_new"); err == nil || err.Error() == "" {
		t.Fatal("未登记的 key 也必须给出非空错误")
	}
}
