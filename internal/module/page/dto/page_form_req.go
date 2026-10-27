package pagedto

import "net/url"

// 页面面（浏览器原生表单 POST）的入参。
//
// 为什么不复用接口面的 *Req：接口面契约带 binding:"required"，缺字段时 binding.Form
// 直接写 400 JSON；而页面面是 <form method="post">，缺字段要走整页提示 —— 把运营丢到
// 一块只有一行字的 400 JSON 上，等于把他刚填的内容一起丢掉。所以这一组的 tag 只有
// form（不带 required），必填校验留在 handler 里，按场景说人话（「没选工程」与
// 「没填路径」的修法不同，不能合成一句）。
//
// 反过来也成立：页面面的 DTO 不进接口面。两条通道的失败形态不同，一份 DTO 两头用
// 必然把其中一头的行为改掉。

// PageLangFormReq 语言面板三条写动作（排除 / 解除排除 / 重新发布）共用。
type PageLangFormReq struct {
	PageID string `form:"pageId"`
	Lang   string `form:"lang"`
}

// SiteSlotBindFormReq 绑定 / 换绑系统页面槽位。
type SiteSlotBindFormReq struct {
	ProjectID string `form:"projectId"`
	Slot      string `form:"slot"`
	PageID    string `form:"pageId"`
}

// SiteSlotUnbindFormReq 解绑系统页面槽位。
type SiteSlotUnbindFormReq struct {
	ProjectID string `form:"projectId"`
	Slot      string `form:"slot"`
}

// ProjectCreateFormReq 新建站点工程（表单只有一个名称字段）。
type ProjectCreateFormReq struct {
	Name string `form:"name"`
}

// PageCreateFormReq 新建页面：工程必填，路径必填，蓝图可选。
type PageCreateFormReq struct {
	ProjectID   string `form:"projectId"`
	DraftPath   string `form:"draftPath"`
	BlueprintID string `form:"blueprintId"`
}

// PageDeleteFormReq 单条删除页面。
type PageDeleteFormReq struct {
	ID string `form:"id"`
}

// PageTranslationsSaveFormReq 译文工作台保存表单。
//
// 行数据是四列**平行数组**（rowContext / rowSource / rowHash / rowTarget）：
// 反射绑定只能按下标拆成四个独立切片，而业务上它们必须整体校验长度
// （错位一格就会把 A 行的原文写进 B 行的译文）。所以这里实现 FormBinder，
// 一次收齐；长度校验留在 handler（不通过时要原地重渲工作台并逐行报错，
// 不能打成 400 JSON —— 那会把编辑者刚填的整屏译文清掉）。
type PageTranslationsSaveFormReq struct {
	PageID     string `form:"pageId"`
	Lang       string `form:"lang"`
	RowContext []string
	RowSource  []string
	RowHash    []string
	RowTarget  []string
}

// BindForm 收齐四列平行数组（顺序即表单里的行顺序）。
func (r *PageTranslationsSaveFormReq) BindForm(v url.Values) error {
	r.RowContext = append([]string(nil), v["rowContext"]...)
	r.RowSource = append([]string(nil), v["rowSource"]...)
	r.RowHash = append([]string(nil), v["rowHash"]...)
	r.RowTarget = append([]string(nil), v["rowTarget"]...)
	return nil
}

// PageHistoryRestoreFormReq 工作台「恢复指定修订」。
//
// Version 收成字符串再在 handler 里 ParseInt：绑定器的数字解析失败会把整张表打成
// 400 JSON，而这里需要的是「参数不对」的整页提示（同一出口、同一句话）。
type PageHistoryRestoreFormReq struct {
	PageID  string `form:"pageId"`
	Version string `form:"version"`
}
