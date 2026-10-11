package adminhttp

// admin_jump.go — admin 后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302/303 + `?err=` / `?done=` / `?saved=` / `?errored=` 回列表页：那条通道要求
// 读侧再判一次「这条提示是不是本仓给的」（adminPageErrText / adminErrTexts / adminPageDone /
// adminPageSaved / adminPageErrParamClean 就是那套），而查询参数不是可信边界。
// 文案改走响应体之后，那套判定整批删除（见 admin_err.go 的说明）。
//
// 四条边界（同 shell.RenderJump 的注释）：
//   · 结论是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 控制器不取词，
//     取词由模板注入的 tf 做（见 adminui.Prepare）。本包里的 adminui.Msg 就是那条事实的形状；
//   · 文案必须**已过本模块的判定 / 已归口**（adminErrMsg 的产物）—— 原文只进日志，
//     换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/shell"
)

// 后台列表页路径（回跳目标）。edit 页（数据规则）另拼 id，不在这里。
const (
	adminAdminsPath    = "/admin/administrators"
	adminRolesPath     = "/admin/roles"
	adminMenusPath     = "/admin/menus"
	adminPermsPath     = "/admin/permissions"
	adminDeptsPath     = "/admin/departments"
	adminDatarulesPath = "/admin/datarules"
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
	path string
	// back 链接文字 / 页面标题的**事实**（key + 中文兜底）：复用各页标题词条，
	// 不新增全站词条。它是包级静态值，所以不需要 gin.Context —— 「这段文案属于哪个语言」
	// 由模板取词时决定，与控制器无关（这正是本批的形状要求）。
	back adminui.Msg
	keys []string
}

// backPath 回跳地址：path + 从本次请求 query 按 keys 透传的筛选。
func (p adminPageRef) backPath(c *gin.Context) string {
	return shell.BackPath(c, p.path, p.keys...)
}

// listQuery 表单 action 上要带的筛选查询串（渲染时拼、POST 回来时由 backPath 读回）。
func (p adminPageRef) listQuery(c *gin.Context) string {
	return adminListQuery(c, p.keys...)
}

// jump 渲染整页提示：成功 1 秒后自动回跳，失败不自动跳（运营要看清楚原因）。
func (p adminPageRef) jump(c *gin.Context, ok bool, msg adminui.Msg) {
	adminJump(c, ok, msg, p.backPath(c), p.back)
}

// done 单条写动作的成功回执（复用全站 MsgSuccess 词条，不新增）。
func (p adminPageRef) done(c *gin.Context) {
	p.jump(c, true, adminSuccessMsg)
}

// fail 失败回执：msg 必须是**已过本模块判定 / 已归口**的事实（adminErrMsg 的产物）。
func (p adminPageRef) fail(c *gin.Context, msg adminui.Msg) {
	p.jump(c, false, msg)
}

// paramFail 参数级校验失败的页面出口（缺 id / 缺必填字段）。
func (p adminPageRef) paramFail(c *gin.Context) {
	p.fail(c, adminBadRequestMsg)
}

// writeFail service 写失败（含「查不到」这类业务判定）的页面出口。
func (p adminPageRef) writeFail(c *gin.Context, err error) {
	p.fail(c, adminErrMsg(c, err))
}

var (
	adminAdminsPage    = adminPageRef{path: adminAdminsPath, back: adminAdminsBackMsg, keys: adminAdminsBackKeys}
	adminRolesPage     = adminPageRef{path: adminRolesPath, back: adminRolesBackMsg, keys: adminRolesBackKeys}
	adminMenusPage     = adminPageRef{path: adminMenusPath, back: adminMenusBackMsg, keys: adminMenusBackKeys}
	adminPermsPage     = adminPageRef{path: adminPermsPath, back: adminPermsBackMsg, keys: adminPermsBackKeys}
	adminDeptsPage     = adminPageRef{path: adminDeptsPath, back: adminDeptsBackMsg, keys: adminDeptsBackKeys}
	adminDatarulesPage = adminPageRef{path: adminDatarulesPath, back: adminDatarulesBackMsg, keys: adminDatarulesBackKeys}
)

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——
var (
	adminAdminsBackMsg    = adminui.Msg{Key: pagesMsgAdministratorsTitle, Fallback: "管理员"}
	adminRolesBackMsg     = adminui.Msg{Key: pagesMsgRolesTitle, Fallback: "角色管理"}
	adminMenusBackMsg     = adminui.Msg{Key: pagesMsgMenusTitle, Fallback: "菜单管理"}
	adminPermsBackMsg     = adminui.Msg{Key: pagesMsgPermissionsTitle, Fallback: "权限资源"}
	adminDeptsBackMsg     = adminui.Msg{Key: pagesMsgDepartmentsTitle, Fallback: "部门管理"}
	adminDatarulesBackMsg = adminui.Msg{Key: pagesMsgDatarulesTitle, Fallback: "数据权限"}

	// adminSuccessMsg 单条写动作的成功回执（复用全站 MsgSuccess 词条，不新增）。
	adminSuccessMsg = adminui.Msg{Key: adminenums.MsgSuccess, Fallback: "操作成功"}
)

// adminJump 渲染整页提示（成功 OK:true + 1 秒自动跳；失败不自动跳）。
//
// 结论是事实：正文与回跳链接文字都按 key 交给模板取词，参数由调用点按计数给。
func adminJump(c *gin.Context, ok bool, msg adminui.Msg, back string, backText adminui.Msg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

// adminBulkIDsMsg 批量 id 超限的**事实**：文案由共享的受控出口取（它只认类型、按当前语言
// 拼出「一次最多操作 N 项，当前 M 项，请分批进行」）。
//
// Key 留空：那句话是共享出口按两个数字拼好的成品串，一段 key 表达不了它
// （与 page 的 pageTranslationsSavedMsg 同一形态）。
func adminBulkIDsMsg(c *gin.Context, berr error) adminui.Msg {
	return adminui.Msg{Fallback: adminui.BulkIDsFacingText(c, berr)}
}

// adminBulkOutcome 批量删除的结论：全成功 → (true, 成功文案)；有跳过 / 无可操作项 → (false, 说明)。
//
// 与改造前的 adminBulkResultURL **逐字同源**（同一批模板与名词）：改造只换传输通道
// （URL → 响应体）与形状（拼好的整句 → 事实），不换「成功说的是哪句话」。既有语义一并保留 ——
// 「一个可操作 id 都没有」也走失败档并说明原因，不让用户对着没变化的列表页猜。
func adminBulkOutcome(c *gin.Context, noun adminui.Msg, deleted, skipped int) (bool, adminui.Msg) {
	nounText := adminui.MsgText(c, noun)
	switch {
	case skipped > 0:
		return false, adminBulkPartialMsg.WithArgs(strconv.Itoa(deleted), nounText, strconv.Itoa(skipped))
	case deleted > 0:
		return true, adminBulkDoneMsg.WithArgs(strconv.Itoa(deleted), nounText)
	}
	// 既没删也没跳过 = 请求里没有一个可用的 id（没勾选 / 勾的全是空值）。
	return false, adminBadRequestMsg
}
