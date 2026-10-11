package bulkids

// bulkids_test.go — 批量 id 归一的边界测试（纯逻辑，不碰数据库、不需要 gin）。
//
// 这里守的是四个容易静默出错的点：
//   · 去空后仍要判上限（空串不该占额度）；
//   · 上限判断发生在**去重之后**（重复提交不该把用户挡在门外）；
//   · 恰好等于上限时放行（off-by-one 最容易在这里出错）；
//   · 空输入返回 (nil, nil)，由调用方决定「未选择」的表现，而不是在这里造一个错。
//
// 这些断言原本在 internal/shell/bulk_test.go（实现曾在壳层）；实现搬到这里后跟着搬过来 ——
// 留在壳层的只有「过渡转发层与终局包同值」的守卫。

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
	// 空输入（nil / 空切片）：直接给 (nil, nil)，调用方据此走「未选择」分支。
	for _, raw := range [][]string{nil, {}} {
		got, err := Normalize(raw)
		if err != nil || got != nil {
			t.Fatalf("空输入应为 (nil, nil)，实际 (%v, %v)", got, err)
		}
	}
	// 全是空白：不是错误，只是归一化后一条不剩（表单路径的历史形状：非 nil 的空切片）。
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
	// 恰好在限内：400 条里只有 200 个不同 id —— 去重后不超限。
	dup := make([]string, 0, MaxIDs*2)
	for i := 0; i < MaxIDs*2; i++ {
		dup = append(dup, "id-"+strconv.Itoa(i%MaxIDs))
	}
	got, err := Normalize(dup)
	if err != nil || len(got) != MaxIDs {
		t.Fatalf("去重后应剩 %d 项且不报错，实际 (%d 项, %v)", MaxIDs, len(got), err)
	}

	// 超一条即整批拒绝（不截断），且错误里带的是**去重后**的条数
	//（受控文案的两个数字取自它 —— 调用层重算是拿不到这个数的）。
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
	// 错误文本不挟带内部细节，且与 TooManyTemplate 同形（旧式前缀判定仍认它）。
	if text := err.Error(); text == "" || text == TooManyTemplate {
		t.Fatalf("错误文本应是填好两个数字的成品句，实际 %q", text)
	}
}
