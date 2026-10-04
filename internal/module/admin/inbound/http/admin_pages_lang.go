package adminhttp

// admin_pages_lang.go — 后台语言切换（GET /admin/lang）：写语言 Cookie 并回跳，回跳地址只允许站内路径。

import (
	"net/http"
	"strings"

	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// --- 语言切换 ---

// AdminLangSwitch 处理 GET /admin/lang：写语言 Cookie 后 302 回跳。
//
// query 参数：
//   - lang：目标语言（zh / zh-CN / en / en-US 等变体，经 response.NormalizeLang 规范化）；
//     缺失或非法时忽略该值并回退配置默认语言（不报错）。
//   - redirect：回跳地址；仅接受站内路径，否则回首页，防开放重定向。
//
// GET 属安全方法，CSRF 直接放行；Cookie 为 SameSite=Lax + Path=/。
func AdminLangSwitch(c *gin.Context) {
	lang, _ := response.NormalizeLang(c.Query("lang"))
	response.SetLangCookie(c, lang)

	c.Redirect(http.StatusFound, adminSafeLangRedirect(c.Query("redirect")))
}

// adminSafeLangRedirect 校验回跳地址：只允许站内相对路径，其余一律回首页 "/"。
//
// 判据**只有一份**：shell.LangRedirectPath（渲染侧把值写进隐藏域时调的也是它）。
// 此前这里重抄了一遍判据（adminLangRedirectAllowed），靠 admin_notice_test.go 的对照用例
// 防漂移 —— 两份实现的漂移方向是静默的：放宽 = 多一个开放重定向面，收紧 = 运营切了语言
// 回不到原页面。现在判据归口到 shell，本函数只剩「拒绝时回首页」这一条消费侧语义。
//
// 为什么消费侧**必须**再校验一遍（而不是信任渲染侧写进隐藏域的值）：这个值从 query 带回来，
// 请求方可以直接改（admin/lang?redirect=//evil.example.com）。消费侧拿到的还是**已解码**
// 的值 —— 反斜杠、换行、NUL 都能出现，比渲染侧的 RequestURI 面更宽，而 LangRedirectPath
// 的判据本身就覆盖了这些（不含反斜杠 / 无控制字符），两边不需要各写一套。
// adminHomePath 控制面首页（仪表盘）—— 与 shell.adminHomePath 同值。
// / 已归前台首页（站点独占域名根），后台回落不能再指 "/"。
const adminHomePath = "/admin"

func adminSafeLangRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if p := shell.LangRedirectPath(raw); p != "" {
		return p
	}
	return adminHomePath
}
