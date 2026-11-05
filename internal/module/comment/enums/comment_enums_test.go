package commentenums

import (
	"errors"
	"testing"

	"go_wp/pkg/facing"
)

func TestStatusWhitelistMatchesConstants(t *testing.T) {
	got := Statuses()
	want := []string{StatusPending, StatusApproved, StatusRejected, StatusSpam}
	if len(got) != len(want) {
		t.Fatalf("Statuses 长度应为 %d，实际 %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Statuses[%d] = %q，期望 %q", i, got[i], want[i])
		}
		if !IsValidStatus(want[i]) {
			t.Errorf("%q 必须被判为合法状态", want[i])
		}
	}
	for _, bad := range []string{"", "PENDING", "deleted", "pending "} {
		if IsValidStatus(bad) {
			t.Errorf("%q 不该被判为合法状态", bad)
		}
	}
}

func TestReviewableStatusesExcludePending(t *testing.T) {
	if IsReviewableStatus(StatusPending) || IsReviewableStatus(StatusSpam) {
		t.Fatal("审核动作不该接受 pending / spam（它们不是审核判断的结果）")
	}
	if !IsReviewableStatus(StatusApproved) || !IsReviewableStatus(StatusRejected) {
		t.Fatal("通过 / 驳回必须可设")
	}
}

func TestBodyLimitMatchesMigrationCheck(t *testing.T) {
	if MaxBodyLen != 2000 || MinBodyLen != 1 {
		t.Fatalf("正文上限应与迁移 466 的 CHECK 一致（1..2000），实际 %d..%d", MinBodyLen, MaxBodyLen)
	}
	if MaxEntityTypeLen != 40 || MaxEntityIDLen != 64 {
		t.Fatalf("实体类型 / id 长度上限应与迁移 466 的列宽一致（40 / 64），实际 %d / %d",
			MaxEntityTypeLen, MaxEntityIDLen)
	}
}

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrRateLimited)
	if err.Error() != ErrRateLimited {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrRateLimited, err.Error())
	}
	// enums 只声明 key + 状态码：FactOf 走 Source 接口拿到 (key, "")，
	// 所以兜底必须是空串 —— 内嵌任何中文都说明文案又回到了声明处。
	fact, ok := facing.FactOf(err)
	if !ok {
		t.Fatalf("%q 没有被判成业务事实", ErrRateLimited)
	}
	if fact.Key != ErrRateLimited {
		t.Errorf("Key = %q，want %q", fact.Key, ErrRateLimited)
	}
	if fact.Fallback != "" {
		t.Errorf("%q 内嵌了兜底文案 %q —— enums 只声明 key，文案归词库", ErrRateLimited, fact.Fallback)
	}
	if !fact.Business {
		t.Errorf("%q 应标记为业务事实", ErrRateLimited)
	}

	if _, ok := facing.FactOf(errors.New("pq: relation comments does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
