package pagination

// window.go — 分页条的**槽位**：哪几个位置画页码、哪几个位置画省略号。
//
// 这里算的是「显示形状」，不是数据 —— 那为什么放在 pkg 而不是 internal/templates？
// 因为窗口算法是**有主张的策略**，不是形式细节：同一个「显示多少页码」的需求，
// Django 的 get_elided_page_range（默认 on_each_side=3, on_ends=2）、
// Rails/Pagy 的 series(slots:7)、Laravel 的 UrlWindow 给出三种互不相同的窗口。
// 把它留在模板里，意味着每个改样式的人都要重写一遍算法。
//
// 三家主流框架的落点都在「库」而不是「视图」：Laravel 在 Illuminate\Pagination，
// Django 在 django.core.paginator，Pagy 在 gem 里。本包与它们同层。
//
// —— 为什么不参数化（这是踩过坑之后的选择，不是省事）——
//
// Laravel 把窗口宽度暴露成 onEachSide() 之后，issue #32943 报的就是边界组合产生重复页
// （"The number 2 is repeated on both sides"）；Pagy 把「窗口宽度」和「是否固定首尾页」
// 耦合进同一个 slots 数值，于是 slots<7 时行为突变（issue #152：中段正常、首尾多出页码）。
// 本仓只有一种分页条形态、只有一个调用点，参数化是投机需求 —— 而两家都为它付过代价。
// 需要第二种形态时再新增函数，不要给这个函数加参数。

// WindowSize 分页条显示的槽位宽度（含首、尾与当前页及其两侧）。
//
// 与迁移前的 maxPageLinks 逐字一致：窗口为 7 时，首尾固定、当前页居中，
// 两侧各留 (7-3)/2 = 2 个位置，贴边时整段平移。
const WindowSize = 7

// Item 分页条上的一个槽位。
//
// 与 Laravel elements() 的混合数组（`[1=>url, '...', 4=>url]`）差别在**判别方式**而不是语义：
// PHP 那边模板靠 is_string / is_array 猜类型，Jet 这边直接读 bool 字段。
//
// 三个字段恒定有值 —— Jet 访问结构体上不存在的字段会中断整页渲染（map 缺键才返回空串），
// 所以不做「可选键」：Gap 为真时 Page 与 URL 是零值，模板不去读它们。
type Item struct {
	Gap  bool   // 省略号槽位
	Page int    // 页码；Gap 为真时为零值
	URL  string // 翻页链接；Gap 为真或该页为当前页时为空
}

// Window 给出分页条要显示的槽位（只算页码与省略号位置，**不拼 URL**）。
//
// 口径与迁移前的 windowNumbers 逐字一致：
//   - pages <= 1          → 空（单页与空集都不该有分页条）
//   - pages <= WindowSize → 1..pages 全部是页码，没有省略号
//   - 否则                → 首、尾固定，当前页居中；贴边时整段平移，不产生重复页
//
// page 越界先夹紧（与取数前的 ClampPage 同口径）：不然会出现「表格为空、分页条显示第 999 页」。
// 省略号是**数据里的一个槽位**，不是模板的算术 —— 模板因此不需要记录「上一个页码是几」
// 再比较差值，那正是原来 partial 里 {{if n-prev > 1}} 那段。
func Window(page, pages int) []Item {
	if pages <= 1 {
		return nil
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	if pages <= WindowSize {
		out := make([]Item, 0, pages)
		for p := 1; p <= pages; p++ {
			out = append(out, Item{Page: p})
		}
		return out
	}

	half := (WindowSize - 3) / 2 // 预留首、尾、当前页
	lo := page - half
	hi := page + half
	if lo < 2 {
		lo, hi = 2, lo+WindowSize-3
	}
	if hi > pages-1 {
		hi, lo = pages-1, pages-1-(WindowSize-3)
	}

	out := make([]Item, 0, WindowSize)
	out = append(out, Item{Page: 1})
	if lo > 2 {
		out = append(out, Item{Gap: true})
	}
	for p := lo; p <= hi; p++ {
		if p < 2 || p > pages-1 {
			continue
		}
		out = append(out, Item{Page: p})
	}
	if hi < pages-1 {
		out = append(out, Item{Gap: true})
	}
	return append(out, Item{Page: pages})
}
