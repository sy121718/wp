package mediaenums

import (
	"errors"
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

// TestStatusOfDeclaresRealHTTPCodes 钉住每个 media 业务错误声明的状态码。
//
// 这份表就是协议的事实源：handler 只调 response.ErrorStatusAuto，不再自选状态。
// 改动其中任何一条都会改变对外契约，所以逐条断言而不是循环遍历常量名 —— 遍历
// 只能证明「有登记」，证明不了「登记成哪个码」。
func TestStatusOfDeclaresRealHTTPCodes(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{ErrAttachmentNotFound, http.StatusNotFound},

		{ErrAttachmentReferenced, http.StatusConflict},

		{ErrUploadEmpty, http.StatusBadRequest},
		{ErrAttachmentNotImage, http.StatusBadRequest},
		{ErrVariantStorageNotLocal, http.StatusBadRequest},
		{ErrDownloadEmpty, http.StatusBadRequest},
		{ErrDownloadStorageNotLocal, http.StatusBadRequest},
		{ErrReplaceExtMismatch, http.StatusBadRequest},

		// 存储 / IO 失败：用户改输入改不了，是 500 不是 400。
		{ErrUploadFailed, http.StatusInternalServerError},
		{ErrDownloadFailed, http.StatusInternalServerError},
		{ErrReplaceFailed, http.StatusInternalServerError},
		{ErrReplaceStaleNotifyFailed, http.StatusInternalServerError},
	}
	for _, c := range cases {
		if got := StatusOf(c.key); got != c.want {
			t.Errorf("StatusOf(%q) = %d，want %d", c.key, got, c.want)
		}
	}
}

// TestStatusOfUnknownKeyHasNoStatus 守住「未登记 = 0」：出口靠它判断这份声明有没有
// 覆盖到该 key。如果这里返回一个默认值（比如 400），忘登记的 key 就再也不会暴露。
func TestStatusOfUnknownKeyHasNoStatus(t *testing.T) {
	if got := StatusOf("ErrMediaNotDeclared"); got != 0 {
		t.Errorf("未登记的 key 应返回 0，实际 %d", got)
	}
}

// TestErrCarriesDeclaredStatus 证明 Err() 在构造时就把状态码带上了 —— 这是
// handler 敢只调 ErrorStatusAuto 的前提。
func TestErrCarriesDeclaredStatus(t *testing.T) {
	for _, key := range []string{ErrAttachmentNotFound, ErrAttachmentReferenced, ErrUploadEmpty} {
		err := Err(key)
		status, ok := facing.StatusOf(err)
		if !ok {
			t.Fatalf("%q 没有带上声明状态", key)
		}
		if want := StatusOf(key); status != want {
			t.Errorf("%q 的状态 = %d，want %d", key, status, want)
		}
		if !errors.Is(err, Err(key)) {
			t.Errorf("%q 应能被 errors.Is 匹配", key)
		}
		fact, fok := facing.FactOf(err)
		if !fok || !fact.Business {
			t.Errorf("%q 应被判成业务事实", key)
		}
		if fact.Key != key {
			t.Errorf("Key = %q，want %q", fact.Key, key)
		}
		// enums 只声明 key + 状态码，文案归词库；兜底恒为空串。
		if fact.Fallback != "" {
			t.Errorf("%q 内嵌了兜底文案 %q —— 文案应只从词库来", key, fact.Fallback)
		}
	}
}

// TestErrDetailKeepsDeclaredStatusAndTail 守住包装路径的状态码。
//
// 这条守的是本轮真实踩到的坑：service 原来用 fmt.Errorf("%s: %w", key, err) 挂明细，
// 那个形状在 pkg/facing 里既不被 StatusOf 认出（包装层没有 Statused），FactOf 又退成
// 纯文本分支（Fallback == key），于是「被引用不能删」声明成 409、实际对外 400。
// ErrDetail 是唯一正确的包装方式，测试同时钉住状态、Tail 与「不内嵌兜底」。
func TestErrDetailKeepsDeclaredStatusAndTail(t *testing.T) {
	err := ErrDetail(ErrAttachmentReferenced, "被 2 个页面引用（首页、关于）")

	status, ok := facing.StatusOf(err)
	if !ok {
		t.Fatal("ErrDetail 的产物没有保住声明状态 —— 出口会退回「业务事实无状态 → 400」")
	}
	if status != http.StatusConflict {
		t.Errorf("状态 = %d，want %d", status, http.StatusConflict)
	}

	fact, fok := facing.FactOf(err)
	if !fok || !fact.Business {
		t.Fatalf("ErrDetail 的产物应被判成业务事实，实际 %+v", fact)
	}
	if fact.Key != ErrAttachmentReferenced {
		t.Errorf("Key = %q，want %q", fact.Key, ErrAttachmentReferenced)
	}
	if fact.Tail != "被 2 个页面引用（首页、关于）" {
		t.Errorf("Tail = %q，want 明细原文", fact.Tail)
	}
	// 走 Source 分支时兜底恒为空；如果这里拿到 key 本身，说明包装被降级成纯文本了。
	if fact.Fallback != "" {
		t.Errorf("Fallback = %q —— 包装走了纯文本分支，声明状态会丢", fact.Fallback)
	}

	if !errors.Is(err, Err(ErrAttachmentReferenced)) {
		t.Error("ErrDetail 的产物应能被 errors.Is 匹配到声明错误")
	}
}

// TestErrDetailWithoutDetailEqualsPlainErr 空明细退化成不带 Tail 的声明错误。
func TestErrDetailWithoutDetailEqualsPlainErr(t *testing.T) {
	for _, detail := range []string{"", "   "} {
		err := ErrDetail(ErrAttachmentNotFound, detail)
		status, ok := facing.StatusOf(err)
		if !ok || status != http.StatusNotFound {
			t.Errorf("明细为 %q 时状态应仍是 404，实际 %d (ok=%v)", detail, status, ok)
		}
		fact, _ := facing.FactOf(err)
		if fact.Tail != "" {
			t.Errorf("明细为 %q 时不应有 Tail，实际 %q", detail, fact.Tail)
		}
	}
}
