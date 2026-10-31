package adminhttp

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
)

const (
	adminAdminsPath    = "/admin/administrators"
	adminRolesPath     = "/admin/roles"
	adminMenusPath     = "/admin/menus"
	adminPermsPath     = "/admin/permissions"
	adminDeptsPath     = "/admin/departments"
	adminDatarulesPath = "/admin/datarules"
)

var (
	adminAdminsBackKeys    = []string{"name", "email", "page", "limit"}
	adminRolesBackKeys     = []string{"keyword", "page", "limit"}
	adminMenusBackKeys     = []string{"keyword", "page", "limit"}
	adminPermsBackKeys     = []string{"code", "module", "page", "limit"}
	adminDeptsBackKeys     = []string{"keyword", "page", "limit"}
	adminDatarulesBackKeys = []string{"domain", "page", "limit"}
)

func adminListQuery(c *gin.Context, keys ...string) string {
	return binding.ListQuery(c, keys...)
}

type adminPageRef struct {
	path string

	back adminui.Msg
	keys []string
}

func (p adminPageRef) backPath(c *gin.Context) string {
	return shell.BackPath(c, p.path, p.keys...)
}

func (p adminPageRef) listQuery(c *gin.Context) string {
	return adminListQuery(c, p.keys...)
}

func (p adminPageRef) jump(c *gin.Context, ok bool, msg adminui.Msg) {
	adminJump(c, ok, msg, p.backPath(c), p.back)
}

func (p adminPageRef) done(c *gin.Context) {
	p.jump(c, true, adminSuccessMsg)
}

func (p adminPageRef) fail(c *gin.Context, msg adminui.Msg) {
	p.jump(c, false, msg)
}

func (p adminPageRef) paramFail(c *gin.Context) {
	p.fail(c, adminBadRequestMsg)
}

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

var (
	adminAdminsBackMsg    = adminui.Msg{Key: pagesMsgAdministratorsTitle, Fallback: "管理员"}
	adminRolesBackMsg     = adminui.Msg{Key: pagesMsgRolesTitle, Fallback: "角色管理"}
	adminMenusBackMsg     = adminui.Msg{Key: pagesMsgMenusTitle, Fallback: "菜单管理"}
	adminPermsBackMsg     = adminui.Msg{Key: pagesMsgPermissionsTitle, Fallback: "权限资源"}
	adminDeptsBackMsg     = adminui.Msg{Key: pagesMsgDepartmentsTitle, Fallback: "部门管理"}
	adminDatarulesBackMsg = adminui.Msg{Key: pagesMsgDatarulesTitle, Fallback: "数据权限"}

	adminSuccessMsg = adminui.Msg{Key: adminenums.MsgSuccess, Fallback: "操作成功"}
)

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

func adminBulkIDsMsg(c *gin.Context, berr error) adminui.Msg {
	return adminui.Msg{Fallback: adminui.BulkIDsFacingText(c, berr)}
}

func adminBulkOutcome(c *gin.Context, noun adminui.Msg, deleted, skipped int) (bool, adminui.Msg) {
	nounText := adminui.MsgText(c, noun)
	switch {
	case skipped > 0:
		return false, adminBulkPartialMsg.WithArgs(strconv.Itoa(deleted), nounText, strconv.Itoa(skipped))
	case deleted > 0:
		return true, adminBulkDoneMsg.WithArgs(strconv.Itoa(deleted), nounText)
	}

	return false, adminBadRequestMsg
}
