package utils

import "testing"

func TestPageOffset(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	cases := []struct {
		name string
		page int
		size int
		want int
	}{
		{"页码0按1算", 0, 20, 0},
		{"页码负数按1算", -7, 20, 0},
		{"第一页", 1, 20, 0},
		{"第二页", 2, 20, 20},
		{"页大小0", 5, 0, 0},
		{"页大小负数", 5, -10, 0},
		{"页大小100", 3, 100, 200},
		{"page极大溢出饱和", maxInt / 2, 100, maxInt},
		{"可表示边界", maxInt, 1, maxInt - 1},
		{"溢出正好越界", maxInt/4 + 2, 4, maxInt},
		{"一页一整型", 2, maxInt, maxInt},
		{"正整数边界不溢出", 3, 4, 8},
	}
	for _, c := range cases {
		if got := PageOffset(c.page, c.size); got != c.want {
			t.Errorf("%s：PageOffset(%d, %d) = %d，期望 %d", c.name, c.page, c.size, got, c.want)
		}
	}
}

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
