package adminhttp

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"
)

const adminErrScene = "admin"

const adminServerErrEvent = "admin 接口内部错误"

func adminInternalMsg() adminui.Msg {
	return adminui.Msg{Key: adminenums.ErrInternal, Fallback: "操作失败，请稍后重试"}
}

var adminParamFallbacks = map[string]string{
	adminenums.ErrAccountLocked: "账号已被锁定，请 %s 后重试",
}

func adminErrMsg(c *gin.Context, err error) adminui.Msg {
	msg, _ := adminErrOutcome(c, err)
	return msg
}

func adminErrOutcome(c *gin.Context, err error) (adminui.Msg, bool) {
	return adminErrOutcomeFact(c, err, adminui.PageErrorFact)
}

func adminErrOutcomeForAPI(c *gin.Context, err error) (adminui.Msg, bool) {
	return adminErrOutcomeFact(c, err, adminAPIErrFact)
}

func adminErrOutcomeFact(c *gin.Context, err error,
	factFn func(*gin.Context, string, error) adminui.ErrFact) (adminui.Msg, bool) {
	if err == nil {
		return adminInternalMsg(), false
	}
	if key, args, ok := adminParamParts(err); ok {
		return adminui.Msg{Key: key, Fallback: adminParamFallback(key), Args: args}, true
	}
	fact := factFn(c, adminErrScene, err)
	if !fact.Business {
		return adminInternalMsg(), false
	}
	return adminui.Msg{Key: fact.Key, Fallback: fact.Fallback}, true
}

func adminAPIErrFact(c *gin.Context, scene string, err error) adminui.ErrFact {
	fact := adminui.PageErrorFactQuiet(c, scene, err)
	if !fact.Business {
		logger.Scene(scene).
			With("path", c.Request.URL.Path).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, adminServerErrEvent)
	}
	return fact
}

func adminParamFallback(key string) string {
	if fb, ok := adminParamFallbacks[key]; ok {
		return fb
	}
	return key
}

func adminParamParts(err error) (key string, args []string, ok bool) {
	raw := strings.TrimSpace(err.Error())
	idx := strings.IndexByte(raw, '|')
	if idx <= 0 {
		return "", nil, false
	}
	key = strings.TrimSpace(raw[:idx])
	if !r.IsBusinessError(errors.New(key)) {
		return "", nil, false
	}
	return key, strings.Split(raw[idx+1:], "|"), true
}

func adminErrWire(msg adminui.Msg) string {
	if msg.Key == "" || len(msg.Args) == 0 {
		return msg.Key
	}
	return msg.Key + "|" + strings.Join(msg.Args, "|")
}

func adminErrText(c *gin.Context, msg adminui.Msg) string {
	if len(msg.Args) > 0 && msg.Fallback == msg.Key {
		return r.TranslateMessage(c, adminErrWire(msg))
	}
	return adminui.MsgText(c, msg)
}

func adminErrParam(c *gin.Context, err error) string {
	msg, _ := adminErrOutcome(c, err)
	return adminErrText(c, msg)
}

func adminFail(c *gin.Context, err error) {
	msg, business := adminErrOutcomeForAPI(c, err)
	if business {
		r.ErrorWithMessage(c, http.StatusBadRequest, adminErrWire(msg))
		return
	}
	r.ErrorWithMessage(c, http.StatusInternalServerError, adminErrWire(msg))
}

func adminWriteFailed(c *gin.Context, err error) {
	adminui.AdminWriteFailedText(c, adminErrParam(c, err))
}

func adminBindFail(c *gin.Context, err error) {
	if err != nil {

		logger.Scene(adminErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			With("detail", err.Error()).
			Warn("admin 接口请求绑定失败")
	}
	r.ErrorWithMessage(c, http.StatusBadRequest, adminenums.MsgBadRequest)
}

func adminListLoadErr(c *gin.Context, loadErr error) string {
	if loadErr == nil {
		return ""
	}
	return adminErrParam(c, loadErr)
}

var (
	adminBulkDoneMsg    = adminui.Msg{Key: adminenums.BulkDoneKey, Fallback: "已删除 %s 个%s"}
	adminBulkPartialMsg = adminui.Msg{Key: adminenums.BulkPartialKey, Fallback: "已删除 %s 个%s，%s 个未能删除（受保护或被引用）"}
)

var (
	adminBulkNounAdmin      = adminui.Msg{Key: adminenums.BulkNounAdmin, Fallback: "管理员"}
	adminBulkNounRole       = adminui.Msg{Key: adminenums.BulkNounRole, Fallback: "角色"}
	adminBulkNounPermission = adminui.Msg{Key: adminenums.BulkNounPermission, Fallback: "权限点"}
	adminBulkNounMenu       = adminui.Msg{Key: adminenums.BulkNounMenu, Fallback: "菜单"}
	adminBulkNounDept       = adminui.Msg{Key: adminenums.BulkNounDept, Fallback: "部门"}
	adminBulkNounDatarule   = adminui.Msg{Key: adminenums.BulkNounDatarule, Fallback: "数据规则"}

	adminBulkNouns = []adminui.Msg{
		adminBulkNounAdmin, adminBulkNounRole, adminBulkNounPermission,
		adminBulkNounMenu, adminBulkNounDept, adminBulkNounDatarule,
	}
)

var adminBadRequestMsg = adminui.Msg{Key: adminenums.MsgBadRequest, Fallback: "请求参数错误"}
