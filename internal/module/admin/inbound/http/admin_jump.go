package adminhttp

// admin_jump.go — admin 后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302/303 + `?err=` / `?done=` / `?saved=` / `?errored=` 回列表页：那条通道要求
// 读侧再判一次「这条提示是不是本仓给的」（adminPageErrText / adminErrTexts / adminPageDone /
// adminPageSaved / adminPageErrParamClean 就是那套），而查询参数不是可信边界。
// 文案改走响应体之后，那套判定整批删除（见 admin_err.go 的说明）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（adminErrParam / shell.BulkIDsFacingText 的产物）——
//     原文只进日志，换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/response"
)

// 后台列表页路径（回跳目标）。edit 页（数据规则）另拼 id，不在这里。
const (
	adminAdminsPath    = "/admin/administrators"
	adminRolesPath     = "/admin/roles"
	adminMenusPath     = "/admin/menus"
	adminPermsPath     = "/admin/permissions"
	adminDeptsPath     = "/admin/departments"
	adminDatarulesPath = "/admin/datarules"
	adminI18nPath      = "/admin/i18n"
)

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」——页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	adminAdminsBackKeys    = []string{"name", "email", "page", "limit"}
	adminRolesBackKeys     = []string{"keyword", "page", "limit"}
	adminMenusBackKeys     = []string{"keyword", "page", "limit"}
	adminPermsBackKeys     = []string{"code", "module", "page", "limit"}
	adminDeptsBackKeys     = []string{"keyword", "page", "limit"}
	adminDatarulesBackKeys = []string{"domain", "page", "limit"}
	// adminI18nBackKeys 词条页的筛选键：列表页用 `lang` 承载语言筛选（编辑抽屉里
	// 词条自身的 lang 占用同一个参数名，那一侧改用 filter_lang，转换见 adminI18nEditFormQuery）。
	adminI18nBackKeys = []string{"keyword", "lang", "category", "page", "limit"}
)

// adminQueryString 把一组键值拼成查询串（空值丢弃、编码走 url.Values：键有序、产物稳定）。
func adminQueryString(values map[string]string) string {
	q := url.Values{}
	for k, v := range values {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	return q.Encode()
}

// adminListQuery 本次请求 query 里的列表上下文（表单 action 上带回来的那一段）。
//
// 同一份实现同时服务「渲染时拼进表单 action」与「失败重渲片段时再拼一次」：两者产出的形状
// 必须一致，否则会出现「首屏表单 action 带 A、失败重渲后带 B」。
func adminListQuery(c *gin.Context, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	in := c.Request.URL.Query()
	values := make(map[string]string, len(keys))
	for _, k := range keys {
		values[k] = strings.TrimSpace(in.Get(k))
	}
	return adminQueryString(values)
}

// adminPageRef 一个后台列表页的出口上下文：回跳路径、链接文字、筛选键表。
//
// 三者绑在一起是因为它们必须同源：回跳路径与筛选键分叉的表现是「写完跳回去筛选丢了」；
// 链接文字与页面标题分叉的表现是「提示页上的按钮说去 A、实际跳 B」。
type adminPageRef struct {
	path     string
	titleKey string
	title    string
	keys     []string
}

// back 回跳地址：path + 从本次请求 query 按 keys 透传的筛选。
func (p adminPageRef) back(c *gin.Context) string {
	return shell.BackPath(c, p.path, p.keys...)
}

// backText 「立即前往」的链接文字（复用各页标题词条，不新增全站词条）。
func (p adminPageRef) backText(c *gin.Context) string {
	return shell.TranslateFor(c)(p.titleKey, p.title)
}

// listQuery 表单 action 上要带的筛选查询串（渲染时拼、POST 回来时由 back 读回）。
func (p adminPageRef) listQuery(c *gin.Context) string {
	return adminListQuery(c, p.keys...)
}

// jump 渲染整页提示：成功 1 秒后自动回跳，失败不自动跳（运营要看清楚原因）。
func (p adminPageRef) jump(c *gin.Context, ok bool, msg string) {
	adminJump(c, ok, msg, p.back(c), p.backText(c))
}

// done 单条写动作的成功回执（复用全站 MsgSuccess 词条，不新增）。
func (p adminPageRef) done(c *gin.Context) {
	p.jump(c, true, shell.TranslateFor(c)(adminenums.MsgSuccess, "操作成功"))
}

// fail 失败回执：msg 必须**已是过白名单 / 已归口**的成品文案。
func (p adminPageRef) fail(c *gin.Context, msg string) {
	p.jump(c, false, msg)
}

// paramFail 参数级校验失败的页面出口（缺 id / 缺必填字段）。
func (p adminPageRef) paramFail(c *gin.Context) {
	p.fail(c, response.TranslateMessage(c, adminenums.MsgBadRequest))
}

// writeFail service 写失败（含「查不到」这类业务判定）的页面出口。
func (p adminPageRef) writeFail(c *gin.Context, err error) {
	p.fail(c, adminErrParam(c, err))
}

var (
	adminAdminsPage    = adminPageRef{path: adminAdminsPath, titleKey: pagesMsgAdministratorsTitle, title: "管理员", keys: adminAdminsBackKeys}
	adminRolesPage     = adminPageRef{path: adminRolesPath, titleKey: pagesMsgRolesTitle, title: "角色管理", keys: adminRolesBackKeys}
	adminMenusPage     = adminPageRef{path: adminMenusPath, titleKey: pagesMsgMenusTitle, title: "菜单管理", keys: adminMenusBackKeys}
	adminPermsPage     = adminPageRef{path: adminPermsPath, titleKey: pagesMsgPermissionsTitle, title: "权限资源", keys: adminPermsBackKeys}
	adminDeptsPage     = adminPageRef{path: adminDeptsPath, titleKey: pagesMsgDepartmentsTitle, title: "部门管理", keys: adminDeptsBackKeys}
	adminDatarulesPage = adminPageRef{path: adminDatarulesPath, titleKey: pagesMsgDatarulesTitle, title: "数据权限", keys: adminDatarulesBackKeys}
	adminI18nPage      = adminPageRef{path: adminI18nPath, titleKey: "admin.i18n.title", title: "文案词条", keys: adminI18nBackKeys}
)

// adminJump 渲染整页提示（成功 OK:true + 1 秒自动跳；失败不自动跳）。
func adminJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// adminBulkOutcome 批量删除的结论：全成功 → (true, 成功文案)；有跳过 / 无可操作项 → (false, 说明)。
//
// 与改造前的 adminBulkResultURL **逐字同源**（同一份 adminBulkTextOf 取词、同一批模板与名词）：
// 改造只换传输通道（URL → 响应体），不换「成功说的是哪句话」。既有语义一并保留 ——
// 「一个可操作 id 都没有」也走失败档并说明原因，不让用户对着没变化的列表页猜。
func adminBulkOutcome(c *gin.Context, noun adminBulkText, deleted, skipped int) (bool, string) {
	nounText := adminBulkTextOf(c, noun)
	switch {
	case skipped > 0:
		tpl := adminBulkTextOf(c, adminBulkPartialText)
		return false, fmt.Sprintf(tpl, strconv.Itoa(deleted), nounText, strconv.Itoa(skipped))
	case deleted > 0:
		tpl := adminBulkTextOf(c, adminBulkDoneText)
		return true, fmt.Sprintf(tpl, strconv.Itoa(deleted), nounText)
	}
	// 既没删也没跳过 = 请求里没有一个可用的 id（没勾选 / 勾的全是空值）。
	return false, response.TranslateMessage(c, adminenums.MsgBadRequest)
}

// —— 词条页专用：编辑抽屉的筛选参数名转换 ——

// adminI18nEditFormQuery 编辑抽屉表单 action 上的列表筛选。
//
// GET 编辑抽屉用 `filter_lang` 承载语言筛选（`lang` 已被词条本身占用），而列表页与回跳用的是
// `lang` —— 参数名转换只在这一处做，避免两处各写一遍（分叉的表现是「保存后语言筛选丢了」）。
func adminI18nEditFormQuery(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	q := c.Request.URL.Query()
	return adminQueryString(map[string]string{
		"keyword":  q.Get("keyword"),
		"lang":     q.Get("filter_lang"),
		"category": q.Get("category"),
		"page":     q.Get("page"),
	})
}

// adminI18nSavedText 词条保存成功回执：复用既有词条 admin.i18n.saved（「已保存：」），
// 后面接写侧构造的 `<key> · <lang>` 身份串；身份串不合形状（拿不到）时退回全站成功文案。
func adminI18nSavedText(c *gin.Context, key, lang string) string {
	lead := shell.TranslateFor(c)("admin.i18n.saved", "已保存：")
	if identity := adminI18nEntryIdentity(key, lang); identity != "" {
		return lead + identity
	}
	return shell.TranslateFor(c)(adminenums.MsgSuccess, "操作成功")
}

// adminI18nDeletedText 词条删除成功回执。`admin.i18n.deleted` 尚未 seed，取词回落中文兜底
// （与 plugin / inventory 新增回执同一处置：词条登记前行为与改造前逐字一致）。
func adminI18nDeletedText(c *gin.Context, key, lang string) string {
	lead := shell.TranslateFor(c)("admin.i18n.deleted", "已删除：")
	if identity := adminI18nEntryIdentity(key, lang); identity != "" {
		return lead + identity
	}
	return shell.TranslateFor(c)(adminenums.MsgSuccess, "操作成功")
}
