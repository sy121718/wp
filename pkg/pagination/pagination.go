package pagination

type Page struct {
	Total int64
	Page  int
	Limit int

	PageParam string
	DropLimit bool
	Base      string
}

// WithBase 返回一份只把 Base 换掉的副本，作为分页取址的**纯展示**能力。
//
// 为什么需要它：Base 是「这一页的分页链接该挂在哪个地址上」的展示决定（窗口与
// 链接由模板算），而派生结构 Derived 上的 WithBase 只对 Derived 有效 —— 页面的
// data 里装的是 Page，模板拿不到把 Base 装回 Page 的办法。补这一个值接收者方法，
// 让模板能在自己的上下文里拼好 Base 再交给公共分页片段。
//
// 值接收者：入参是副本，只改副本的 Base，调用方持有的 Page 一个字段都不动
// （分页事实是只读输入，改它会是隐蔽的串页）。
func (p Page) WithBase(base string) Page {
	p.Base = base
	return p
}
