package sysconfighttp

// sysconfig_err.go — 页面路径的错误归口（与 admin / project / page 同形）。
//
// 三件套：① 白名单（sysconfigenums.SysConfigFacingMessages，由模块 AST 对账测试钉住）；
// ② 归口文案（ErrInternal）；③ 结构化日志。页面写动作的结论由 sysconfigJump 渲染成提示页。

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/internal/shell"

	"go_wp/pkg/logger"
)

// sysconfigFacingMessages 白名单集合（值 → true）。
var sysconfigFacingMessages = func() map[string]bool {
	m := make(map[string]bool, len(sysconfigenums.SysConfigFacingMessages))
	for _, v := range sysconfigenums.SysConfigFacingMessages {
		m[v] = true
	}
	return m
}()

// sysconfigErrText 取可展示的文案（当前语言的译文）。
func sysconfigErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	// 哨兵优先：ErrGroupNotFound 在契约里是语义值（读侧要据此回退），在页面上的
	// 文案取自 enums 同名常量。
	if errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
		return shell.TranslateFor(c)(sysconfigenums.ErrGroupNotFound, "配置分组不存在")
	}
	key := err.Error()
	if sysconfigFacingMessages[key] {
		return shell.TranslateFor(c)(key, key)
	}
	// 未命中白名单 → 归口文案 + 原文只进日志（否则 PostgreSQL 原文会经 ?err= 透出）。
	logger.Scene("sysconfig").Error(err, "系统设置页面操作失败（非业务错误，只对外给归口文案）")
	return shell.TranslateFor(c)(sysconfigenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）。")
}

// sysconfigPagePath 系统设置页路径（写动作的回跳目标）。
const sysconfigPagePath = "/admin/system"

// sysconfigJump 写动作的出口：整页提示（对应 ThinkPHP 的 success() / error()）。
//
// 提示文本必须**已过本模块白名单 / 已归口**（sysconfigErrText 的产物），
// 原文只进日志 —— 换个页面呈现不等于可以把 err.Error() 铺在页面上。
//
// 取代了原先的 303 + `?err=` / `?done=1`：那条通道要求读侧再判一次「这条提示是不是本仓给的」，
// 而查询参数不是可信边界。现在文案走响应体，读侧判定与 sysconfigRedirect 一起消失。
func sysconfigJump(c *gin.Context, ok bool, msg string) {
	back := sysconfigPagePath
	backText := shell.TranslateFor(c)(sysconfigenums.AdminSystemTitle, "系统设置")
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// parseVersion 解析版本号（非法 → 0，service 会以「缺少版本号」拒绝）。
func parseVersion(raw string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}
