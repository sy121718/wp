package pagination

import "testing"

// Page.WithBase 是分页取址的**纯展示**能力：值接收者复制后只改 Base。
//
// 两条断言缺一不可：
//   - 返回值除 Base 外逐字段等于入参（否则「顺手」改了 Limit/Page 会让分页串页）；
//   - 入参本身一个字段都没变（Go 里值接收者天生如此，但这条是回归哨兵：
//     一旦有人把接收者改成指针，分页事实就会被静默改写）。
func TestPageWithBaseReturnsCopyAndKeepsOriginal(t *testing.T) {
	original := Page{
		Total:     120,
		Page:      3,
		Limit:     20,
		PageParam: "p",
		DropLimit: true,
		Base:      "/admin/x?k=1",
	}
	before := original

	got := original.WithBase("/admin/y?k=2")

	if got.Base != "/admin/y?k=2" {
		t.Fatalf("返回值的 Base 应被换成新值，实际 %q", got.Base)
	}

	want := before
	want.Base = "/admin/y?k=2"
	if got != want {
		t.Fatalf("除 Base 之外的字段必须原样保留\n got: %+v\nwant: %+v", got, want)
	}

	if original != before {
		t.Fatalf("入参不能被改写（值接收者语义）\n got: %+v\nwant: %+v", original, before)
	}
}

// 空 Base 也是合法输入：清空取址不是「调用失败」。
func TestPageWithBaseAcceptsEmptyBase(t *testing.T) {
	got := Page{Total: 1, Page: 1, Limit: 10, Base: "/admin/x"}.WithBase("")
	if got.Base != "" {
		t.Fatalf("空 Base 应被原样写入，实际 %q", got.Base)
	}
	if got.Total != 1 || got.Page != 1 || got.Limit != 10 {
		t.Fatalf("空 Base 不该影响其他字段：%+v", got)
	}
}
