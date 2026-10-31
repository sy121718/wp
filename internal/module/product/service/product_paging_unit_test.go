package productservice

import "testing"

func TestOptionalPagingPreservesIntentAndOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name                      string
		page, size, limit, offset int
	}{
		{"全量", 0, 0, 0, 0},
		{"负值全量", -1, -1, 0, 0},
		{"默认页", 0, 4, 4, 0},
		{"默认大小", 2, 0, defaultPageSize, defaultPageSize},
		{"封顶", 2, maxPageSize + 1, maxPageSize, maxPageSize},
		{"偏移溢出", maxInt/4 + 2, 4, 4, maxInt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit, offset := optionalPaging(tc.page, tc.size)
			if limit != tc.limit || offset != tc.offset {
				t.Fatalf("分页结果=(%d,%d),期望=(%d,%d)", limit, offset, tc.limit, tc.offset)
			}
		})
	}
}
