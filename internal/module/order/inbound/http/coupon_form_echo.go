package orderhttp

// coupon_form_echo.go — 优惠码新建/编辑表单的「写失败不丢输入」分档出口。
//
// 契约与判据：internal/templates/CLAUDE.md「写表单失败的『原地留住输入』分档」、
// docs/02-T-write-fail-echo-batch1.md；实现样板：product 的 productCreateFail /
// productCreateFormFields（product_page_new.go）。分档工具在本包内自成一份
//（跨包私有函数不可复用；product / inventory 同样各自持有一份，属既有结构），
// 全部带 coupon 前缀，避免与同包并行改动撞名。
//
// 抽屉形态的现实（与父会话对齐后的口径）：新建/编辑表单只住在 <template> 里，
// 唯一入口是 data-drawer-open（JS 抽屉克隆内容），无 JS 时这两个表单不可达 ——
// htmx 是唯一真实提交通道，分档只保留一条兜底而不做双轨：
//
//	失败：htmx（HX-Request: true）→ 200 + 片段自身（错误槽 + 回填后的表单）；
//	      其余（手工构造的原生请求）→ 现状 302 + ?err= 回列表页，不新增行为。
//	成功：htmx → HX-Redirect（htmx 的 XHR 会自己跟随 302，最终响应里读不到
//	      Location，整页 HTML 会被塞进片段位置）；其余 → 现状 302。
//
// 抽屉与 htmx 的配合全部是现成机制，不需要新增 JS：
//   - drawer.js 打开抽屉时对克隆内容 htmx.process(body)，hx-post 才被认领；
//   - 失败片段的 host div 被 outerHTML 替换发生在 [data-drawer-body] 内部，
//     抽屉容器本身不动 → 回填后的表单出现时抽屉仍保持打开；
//   - ui/index.js 挂了 htmx:afterSwap 重扫（WBUI.scan），换入片段里的原生控件照常增强。

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
)

// couponCreateFormFields 新建表单片段的回填字段清单（片段与 handler 之间的协议）。
//
// 与 coupon_create_form.html 里的 name= 一一对应，双向一致性由
// TestCouponCreateFormFieldsMatchTemplate 双向钉住。漏列的字段在失败片段里读不到
// —— Jet 读 context 上缺失的键会中断渲染（500，htmx 判 swap:false），
// 现象是「点了保存什么都看不到」。
var couponCreateFormFields = []string{
	"projectId", "returnQuery", "code", "name", "discountType", "discountValue",
	"minSubtotal", "maxUses", "perUserLimit", "status", "startsAt", "endsAt", "remark",
}

// couponEditFormFields 编辑表单片段的回填字段清单（与 coupon_edit_form.html 对应）。
//
// 刻意**没有** code：编辑表单的券码是 readonly 且无 name（「券码不可改」的协议形态，
// 提交里本来就不带它），失败片段里的券码由 couponEditFail 经 GetCoupon 取
// （EditCode 键），不进回填清单。
var couponEditFormFields = []string{
	"id", "projectId", "returnQuery", "name", "discountType", "discountValue",
	"minSubtotal", "maxUses", "perUserLimit", "status", "startsAt", "endsAt", "remark",
}

// couponEchoMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
const couponEchoMemory = 32 << 20

// couponFormEcho 这次请求的表单快照（失败片段回填用）。
//
// 值只来自**这次提交**，不回查数据库：回填要的是「用户刚打的字」，而不是「库里的旧值」。
type couponFormEcho struct {
	// values 原样保存提交值（含空值与首尾空白），多值保留提交顺序。
	values url.Values
}

// couponFormEchoFrom 取这次请求的表单快照。
//
// 走标准库的解析入口（gin 的 PostForm 也走这里），urlencoded 与 multipart 都会填进
// PostForm；解析错误一律忽略 —— 解析失败就当「没提交」，回填退化成空表单，
// 提交是否合法由各 handler 的业务校验回答，不在这里变成 500。
func couponFormEchoFrom(c *gin.Context) couponFormEcho {
	vals := url.Values{}
	if c == nil || c.Request == nil {
		return couponFormEcho{values: vals}
	}
	_ = c.Request.ParseMultipartForm(couponEchoMemory)
	for key, list := range c.Request.PostForm {
		vals[key] = append([]string(nil), list...)
	}
	return couponFormEcho{values: vals}
}

// value 单值字段的回填值（缺失返回空串）：文本 / 下拉 / 隐藏域用。
func (e couponFormEcho) value(name string) string {
	if list := e.values[name]; len(list) > 0 {
		return list[0]
	}
	return ""
}

// list 多值字段的完整回填值（保留提交顺序）。
func (e couponFormEcho) list(name string) []string {
	return e.values[name]
}

// checked 该字段这次是否被提交过（复选框的回填判据）。
func (e couponFormEcho) checked(name string) bool {
	return len(e.values[name]) > 0
}

// couponFormEchoData 把回填值摊成片段 data 可直接并入的三个键。
//
//	"FormEcho"        gin.H{字段名: string}   文本 / 下拉 / 隐藏域 → value="{{ .FormEcho.code }}"
//	"FormEchoChecked" gin.H{字段名: bool}     复选框 → {{if .FormEchoChecked.x}}checked{{end}}
//	"FormEchoMulti"   gin.H{字段名: []string} 多值字段的完整提交值（保序，逐项比对勾选）
//
// **键名必须带 FormEcho 前缀**：片段与页面共用同一份渲染 data，通用键名迟早撞上
// 页面已有的同名键（本页虽暂无 Form，但页面键空间是共享的，撞上即渲染失败）。
// 清单里列出的每个字段都会被补零值 —— 模板读 context 上缺失的键会让渲染中断。
func couponFormEchoData(c *gin.Context, fields ...string) gin.H {
	echo := couponFormEchoFrom(c)
	form := make(gin.H, len(fields))
	checked := make(gin.H, len(fields))
	multi := make(gin.H, len(fields))
	for _, name := range fields {
		form[name] = echo.value(name)
		checked[name] = echo.checked(name)
		multi[name] = echo.list(name)
	}
	return gin.H{"FormEcho": form, "FormEchoChecked": checked, "FormEchoMulti": multi}
}

// couponIsHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 判据与 couponRedirectWhere 同口径（大小写不敏感、去首尾空白）：
// 失败出口与成功出口必须认同同一个信号。
func couponIsHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// couponRedirectWhere 写成功的分档跳转出口。
//
// htmx 请求走 HX-Redirect + 200（htmx 读头整页跳转）；其余走既有 302。
// 两条路的终点是同一个 URL，页面壳与提示位完全一致。
func couponRedirectWhere(c *gin.Context, target string) {
	if couponIsHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, target)
}

// couponEchoQuery 回跳查询串：returnQuery 的白名单键透传 + ok/err 以本次结论覆盖。
//
// 回跳上下文由一个隐藏域 returnQuery 整体承载，而不是 project / status / keyword
// 逐个铺开：券的启停字段就叫 status，与筛选参数同名 —— 逐个铺开的话，
// 表单里那个 status 到底是「回跳筛选」还是「这张券的启停」只能靠猜。
//
// 从 couponRedirectSkip 提出来的公共构造（成功分档与原生 302 共用同一份 URL 语义，
// 不能各拼一份 —— 拼岔了 htmx 档和原生档会落在两个不同的页面上）。
func couponEchoQuery(c *gin.Context, okText, errText string) url.Values {
	q := url.Values{}
	if parsed, err := url.ParseQuery(strings.TrimSpace(c.PostForm("returnQuery"))); err == nil {
		// 只透传白名单键：returnQuery 同样来自客户端，不能让它往回跳 URL 里塞任意参数。
		for key, vals := range parsed {
			if _, ok := couponBackKeys[key]; ok && len(vals) > 0 {
				q.Set(key, vals[0])
			}
		}
	}
	// ok / err 一律以本次操作的结论为准，不采信客户端塞进来的提示。
	q.Del("ok")
	q.Del("err")
	if okText != "" {
		q.Set("ok", okText)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	return q
}

// couponRedirectTarget 写成功的回跳地址（列表页 + 上下文透传 + 本次结论）。
func couponRedirectTarget(c *gin.Context, okText string) string {
	return "/admin/coupons?" + couponEchoQuery(c, okText, "").Encode()
}

// couponCreateFail 新建表单的失败出口（分档口径唯一，别在调用点各写一份头判断）。
//
//	htmx 请求：200 + 片段（错误槽 + 回填后的表单）—— 抽屉里原地留住已填内容；
//	其余请求：现状 302 + ?err= 回列表页（抽屉表单没有无 JS 提交通道，这只是一条兜底）。
func (h *couponPageHandle) couponCreateFail(c *gin.Context, msg string) {
	if !couponIsHXRequest(c) {
		couponRedirect(c, "", msg)
		return
	}
	data := couponFormEchoData(c, couponCreateFormFields...)
	// 失败档的 context 键全部由这里给齐：片段对页面键用 chain/isset 读取，
	// 唯一硬前提是 FormEcho* 三键（couponFormEchoData 已按清单补零值）。
	data["SelectedProject"] = strings.TrimSpace(c.PostForm("projectId"))
	data["TypeOptions"] = couponTypeOptions
	data["StatusOptions"] = couponEnableOptions
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/order/coupon_create_form.html", shell.Prepare(c, data))
}

// couponEditFail 编辑表单的失败出口（分档口径同 couponCreateFail）。
//
// 券码展示（表单不提交 code）从库里取一次；取不到（id 非法、券已被并发删除）时不渲染
// 半个片段 —— 用 HX-Redirect 回列表页，让用户带着错误文案回到还能用的列表，
// 而不是把一张查不到的券的表单留在抽屉里。
func (h *couponPageHandle) couponEditFail(c *gin.Context, msg string) {
	if !couponIsHXRequest(c) {
		couponRedirect(c, "", msg)
		return
	}
	code := ""
	if id := orderQueryID(c.PostForm("id")); id > 0 {
		if cp, err := h.orders.GetCoupon(c.Request.Context(), id); err == nil && cp != nil {
			code = cp.Code
		}
	}
	if code == "" {
		couponRedirectWhere(c, "/admin/coupons?"+couponEchoQuery(c, "", msg).Encode())
		return
	}
	data := couponFormEchoData(c, couponEditFormFields...)
	data["TypeOptions"] = couponTypeOptions
	data["StatusOptions"] = couponEnableOptions
	data["EditCode"] = code
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/order/coupon_edit_form.html", shell.Prepare(c, data))
}
