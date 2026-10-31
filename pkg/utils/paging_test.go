package utils

import "testing"

func TestNormalizePagingSaturatesOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, size := range []int{2, 4, 100} {
		got := NormalizePaging(maxInt/size+2, size, 20, 100)
		if got.Offset != maxInt {
			t.Fatalf("size=%d offset溢出: got=%d want=%d", size, got.Offset, maxInt)
		}
	}
	if got := NormalizePaging(3, 4, 20, 100); got.Offset != 8 {
		t.Fatalf("普通分页改变: %+v", got)
	}
	if got := NormalizePaging(maxInt, 1, 20, 100); got.Offset != maxInt-1 {
		t.Fatalf("可表示边界改变: %+v", got)
	}
}
