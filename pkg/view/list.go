package view

// list.go — 列表页的**分页契约**：视图自己说出「我在第几页、每页多少条、总共多少条」，
// 渲染出口据此把分页条要的事实放进渲染数据。
//
// 为什么需要这个接口，而不是各页面自己往 map 里塞 data["Pagination"]：
//
//	那个键的名字是**字符串字面量**，散在 22 处控制器里（admin 7 处、mail 6 处、order 3 处、
//	user 2 处、analytics/content/masterdata 各 1 处）。写错一个字母不会编译失败，
//	页面只是少一个分页条 —— 而「少一块」在页面上看起来跟「本来就没有」一模一样。
//	接口化之后，视图必须给出这三件事实才能让分页条出现，缺了是编译错误。
//
// 与 Laravel 的对照：Laravel 里 `view('x', ['posts' => $p])` 的键名拼错要到渲染期才炸
// （PHP 没有编译期字段检查）；Go 这里用 struct/接口，拼错在编译期就报。
// 这是「发标准格式就识别」在 Go 里比 PHP 更强的地方，不是更弱。
//
// 与 Result（PageStatus / PageNoStore）同一手法：接口定义在 pkg/view、由业务 dto 实现、
// 渲染出口按接口识别。dto 因此不需要 import 渲染层。

import (
	"go_wp/pkg/pagination"
)

// PageList 列表页视图：给出分页事实即自动获得分页条。
//
// 实现者返回零值 Page 表示「这屏没有分页条」（单页、空集、或本来就不是列表页），
// 渲染出口据此跳过注入 —— 不需要再写一个 `if hasPagination` 判断。
//
// 典型实现（值接收者，字段由 service 填好）：
//
//	func (v AdminListPageResp) Paging() pagination.Page {
//		return pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit, Base: v.Base}
//	}
//
// **方法名是 Paging 而不是 Pagination**：Go 不允许字段与方法同名，而存量 dto 里
// 已经有 `Pagination pagination.Page` 这个字段（product 的 5 个视图都在用）。
// 用 Paging 就能给它们补一个方法而不用改字段名 —— 字段名是模板契约，
// 改名意味着动模板，那是这次不该付的代价。
type PageList interface {
	Paging() pagination.Page
}

// PageListOf 从视图里取分页事实，视图没实现 PageList 时返回零值与 false。
//
// 第二个返回值是「有没有分页条」的**唯一判据**：不实现接口 = 没有分页条，
// 实现了但返回零值 Page（Total=0）= 也没有。两者对模板的效果相同，
// 但成因不同，所以这里分开表达。
func PageListOf(v any) (pagination.Page, bool) {
	if v == nil {
		return pagination.Page{}, false
	}
	l, ok := v.(PageList)
	if !ok {
		return pagination.Page{}, false
	}
	return l.Paging(), true
}

// PaginationKey 分页事实在渲染数据里的键名。
//
// 导出而不是各处写字面量：模板读 `.["Pagination"]`，出口写这个常量，
// 两边引用同一个符号时改名只需要改一处（模板那边由 templates 包的测试钉住）。
const PaginationKey = "Pagination"
