package adminhttp

import (
	"github.com/gin-gonic/gin"

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

// adminListQuery 只透传白名单里的筛选键，其余参数一律丢弃。
func adminListQuery(c *gin.Context, keys ...string) string {
	return binding.ListQuery(c, keys...)
}

// adminPageRef 是列表页的路径与回跳参数白名单。
type adminPageRef struct {
	path string
	keys []string
}

func (p adminPageRef) listQuery(c *gin.Context) string {
	return adminListQuery(c, p.keys...)
}

var (
	adminAdminsPage    = adminPageRef{path: adminAdminsPath, keys: adminAdminsBackKeys}
	adminRolesPage     = adminPageRef{path: adminRolesPath, keys: adminRolesBackKeys}
	adminMenusPage     = adminPageRef{path: adminMenusPath, keys: adminMenusBackKeys}
	adminPermsPage     = adminPageRef{path: adminPermsPath, keys: adminPermsBackKeys}
	adminDeptsPage     = adminPageRef{path: adminDeptsPath, keys: adminDeptsBackKeys}
	adminDatarulesPage = adminPageRef{path: adminDatarulesPath, keys: adminDatarulesBackKeys}
)
