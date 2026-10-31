package adminctx

import (
	admincontract "go_wp/internal/module/admin/contract"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

const (
	PermSetKey = "perm_set"

	NavTreeKey = "nav_tree"

	ButtonsKey = "button_set"
)

func PermContextMiddleware(authz admincontract.AuthzContextService) gin.HandlerFunc {
	return func(c *gin.Context) {
		set := map[string]bool{}
		buttons := map[string]bool{}
		if authz != nil {
			if v, ok := c.Get("user_id"); ok {
				if uid, ok := v.(int64); ok && uid > 0 {
					codes, aerr := authz.EffectivePermissionCodes(c.Request.Context(), uint64(uid))
					if aerr != nil {
						logger.Scene("dashboard").With("userId", uid).
							Error(aerr, "权限上下文查询失败，本页按空权限集渲染")

						codes = nil
					} else {
						for _, code := range codes {
							set[code] = true
						}

						if list, berr := authz.AuthorizedButtonCodes(c.Request.Context(), codes); berr != nil {
							logger.Scene("dashboard").With("userId", uid).
								Error(berr, "按钮码查询失败，本页按钮按无权限渲染")
						} else {
							for _, b := range list {
								buttons[b] = true
							}
						}

						tree, terr := authz.BuildAuthorizedTree(c.Request.Context(), codes)
						if terr != nil {
							logger.Scene("dashboard").With("userId", uid).
								Error(terr, "导航菜单树构建失败，本页侧栏为空")
						} else {
							c.Set(NavTreeKey, tree)
						}
					}
				}
			}
		}
		c.Set(PermSetKey, set)
		c.Set(ButtonsKey, buttons)
		c.Next()
	}
}
