package sysconfighttp

// sysconfig_err.go — 页面路径的错误归口（与 admin / project / page 同形）。
//
// 三件套：① 白名单（sysconfigenums.SysConfigFacingMessages，由模块 AST 对账测试钉住）；
// ② 归口文案（ErrInternal）；③ 结构化日志。写操作返回 JSON 回执。

import (
	"errors"

	"github.com/gin-gonic/gin"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

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
		return i18n.TranslateFunc(response.RequestLanguage(c))(sysconfigenums.ErrGroupNotFound, "配置分组不存在")
	}
	key := err.Error()
	if sysconfigFacingMessages[key] {
		return i18n.TranslateFunc(response.RequestLanguage(c))(key, key)
	}
	// 未命中白名单 → 归口文案 + 原文只进日志（否则 PostgreSQL 原文会经 ?err= 透出）。
	logger.Scene("sysconfig").Error(err, "系统设置页面操作失败（非业务错误，只对外给归口文案）")
	return i18n.TranslateFunc(response.RequestLanguage(c))(sysconfigenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）。")
}
