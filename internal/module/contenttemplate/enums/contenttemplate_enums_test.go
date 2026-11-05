package contenttemplateenums

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

func TestNotFoundDeclares404(t *testing.T) {
	for _, err := range []error{Err(ErrNotFound), ErrDetail(ErrNotFound, "模板 tpl-1")} {
		if status, ok := facing.StatusOf(err); !ok || status != http.StatusNotFound {
			t.Fatalf("模板不存在应携带 404，实际 status=%d ok=%v err=%v", status, ok, err)
		}
	}
}

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrNotFound)
	if err.Error() != ErrNotFound {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrNotFound, err.Error())
	}
	// enums 只声明 key + 状态码：FactOf 走 Source 接口拿到 (key, "")，
	// 所以兜底必须是空串 —— 内嵌任何中文都说明文案又回到了声明处。
	fact, ok := facing.FactOf(err)
	if !ok {
		t.Fatalf("%q 没有被判成业务事实", ErrNotFound)
	}
	if fact.Key != ErrNotFound {
		t.Errorf("Key = %q，want %q", fact.Key, ErrNotFound)
	}
	if fact.Fallback != "" {
		t.Errorf("%q 内嵌了兜底文案 %q —— enums 只声明 key，文案归词库", ErrNotFound, fact.Fallback)
	}
	if !fact.Business {
		t.Errorf("%q 应标记为业务事实", ErrNotFound)
	}
	detail := ErrDetail(ErrNotFound, "模板 tpl-1")
	if !strings.HasPrefix(detail.Error(), ErrNotFound+": ") {
		t.Fatalf("ErrDetail 的明细段必须是半角冒号：%q", detail.Error())
	}
	fact, ok = facing.FactOf(detail)
	if !ok || !fact.Business || fact.Key != ErrNotFound || fact.Tail != "模板 tpl-1" {
		t.Fatalf("ErrDetail 的事实应保留 key 与明细段，实际 %+v ok=%v", fact, ok)
	}
	if _, ok := facing.FactOf(errors.New("pq: relation content_templates does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
