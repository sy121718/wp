package productenums

import (
	"fmt"
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

func TestErrStatusesOnlyCoverDeclaredKeys(t *testing.T) {
	declared := errKeyConstants(t)
	for key, name := range declared {
		if !isErrConstant(name) {
			continue
		}
		status := StatusFor(key)
		switch status {
		case 0, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusInternalServerError:
		default:
			t.Errorf("%s (=%q) 声明了非标准业务状态码 %d", name, key, status)
		}
	}
}

func TestErrFactoryCarriesDeclaredStatus(t *testing.T) {
	cases := []struct {
		key    string
		status int
	}{
		{ErrNotFound, http.StatusNotFound},
		{ErrSlugTaken, http.StatusConflict},
		{ErrNameRequired, 0},
	}
	for _, tc := range cases {
		got, ok := facing.StatusOf(Err(tc.key))
		if tc.status == 0 {
			if ok {
				t.Errorf("Err(%s) 未声明状态，实际带 %d（默认档由出口给）", tc.key, got)
			}
			continue
		}
		if !ok || got != tc.status {
			t.Errorf("Err(%s) 的状态码 = (%d, %v)，want (%d, true)", tc.key, got, ok, tc.status)
		}
	}
}

func TestCompositeErrorKeepsStatus(t *testing.T) {
	err := fmt.Errorf("%w: %s", Err(ErrSlugTaken), "summer-sale")
	if got, ok := facing.StatusOf(err); !ok || got != http.StatusConflict {
		t.Fatalf("复合错误的状态码 = (%d, %v)，want (409, true)", got, ok)
	}
	if fact, ok := facing.FactOf(err); !ok || fact.Key != ErrSlugTaken {
		t.Fatalf("复合错误应保住业务 key，实际 %+v（ok=%v）", fact, ok)
	}
}

func TestErrFactoryDeclaresOnlyKeyAndStatus(t *testing.T) {
	for key, name := range errKeyConstants(t) {
		if !isErrConstant(name) {
			continue
		}
		fact, ok := facing.FactOf(Err(key))
		if !ok || !fact.Business || fact.Key != key {
			t.Errorf("%s：错误事实应为业务 key %q，实际 %+v（ok=%v）", name, key, fact, ok)
			continue
		}
		if fact.Fallback != "" {
			t.Errorf("%s：错误声明不该再带中文兜底（%q）—— 文案归词库，缺词由 JSON 出口给共享提示",
				name, fact.Fallback)
		}
		if fact.Tail != "" {
			t.Errorf("%s：错误事实不该带明细，实际 %q", name, fact.Tail)
		}
	}
}
