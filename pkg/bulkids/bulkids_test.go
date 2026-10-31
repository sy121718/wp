package bulkids

import (
	"errors"
	"strconv"
	"testing"
)

func TestNormalizeDedupesAndTrims(t *testing.T) {
	got, err := Normalize([]string{" a ", "a", "", "  ", "b", "b", "c"})
	if err != nil {
		t.Fatalf("未超限不应报错: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("归一化结果 = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("归一化应保持首次出现顺序：%v，期望 %v", got, want)
		}
	}
}

func TestNormalizeEmptyIsNilNotError(t *testing.T) {

	for _, raw := range [][]string{nil, {}} {
		got, err := Normalize(raw)
		if err != nil || got != nil {
			t.Fatalf("空输入应为 (nil, nil)，实际 (%v, %v)", got, err)
		}
	}

	got, err := Normalize([]string{"", "   "})
	if err != nil || len(got) != 0 {
		t.Fatalf("全空白应归一化成空列表且不报错，实际 (%v, %v)", got, err)
	}
}

func TestNormalizeExactlyAtLimitPasses(t *testing.T) {
	raw := make([]string, 0, MaxIDs)
	for i := 0; i < MaxIDs; i++ {
		raw = append(raw, "id-"+strconv.Itoa(i))
	}
	got, err := Normalize(raw)
	if err != nil || len(got) != MaxIDs {
		t.Fatalf("恰好 %d 项必须放行，实际 (%d 项, %v)", MaxIDs, len(got), err)
	}
}

func TestNormalizeLimitAppliesAfterDedup(t *testing.T) {

	dup := make([]string, 0, MaxIDs*2)
	for i := 0; i < MaxIDs*2; i++ {
		dup = append(dup, "id-"+strconv.Itoa(i%MaxIDs))
	}
	got, err := Normalize(dup)
	if err != nil || len(got) != MaxIDs {
		t.Fatalf("去重后应剩 %d 项且不报错，实际 (%d 项, %v)", MaxIDs, len(got), err)
	}

	over := make([]string, 0, MaxIDs+1)
	for i := 0; i <= MaxIDs; i++ {
		over = append(over, "id-"+strconv.Itoa(i))
	}
	_, err = Normalize(over)
	if !errors.Is(err, ErrTooMany) {
		t.Fatalf("超限应返回 sentinel，实际 %v", err)
	}
	var typed *IDsError
	if !errors.As(err, &typed) || typed.Count != MaxIDs+1 || typed.Max != MaxIDs {
		t.Fatalf("错误应带 Count/Max，实际 %+v", typed)
	}

	if text := err.Error(); text == "" || text == TooManyTemplate {
		t.Fatalf("错误文本应是填好两个数字的成品句，实际 %q", text)
	}
}
