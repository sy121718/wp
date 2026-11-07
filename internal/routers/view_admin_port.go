package routers

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	admindto "go_wp/internal/module/admin/dto"
	pageview "go_wp/pkg/view"
)

// wireAdminViewPort 把后台页面的请求事实来源接到 pkg/view。
//
// pkg 不能 import internal：CSRF 签发与授权菜单树都属于应用侧能力，
// 因此由这里在装配期注入，pkg/view 只持有函数值。
// 菜单树在每次请求按当前用户重新投影，不做全进程缓存。
func wireAdminViewPort() {
	pageview.SetAdminPort(pageview.AdminPort{
		CSRFToken: func(c *gin.Context) string {
			tok, err := builtin.EnsureCSRFToken(c)
			if err != nil {
				return ""
			}
			return tok
		},
		NavTree: func(c *gin.Context) []pageview.AdminNavNode {
			v, ok := c.Get(adminctx.NavTreeKey)
			if !ok {
				return nil
			}
			tree, ok := v.([]admindto.MenuTreeNode)
			if !ok {
				return nil
			}
			return adminNavNodes(tree)
		},
	})
}

// adminNavNodes 把授权菜单树投影成渲染事实（隐藏项在这里就剔除，模板不再判）。
func adminNavNodes(nodes []admindto.MenuTreeNode) []pageview.AdminNavNode {
	out := make([]pageview.AdminNavNode, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		out = append(out, pageview.AdminNavNode{
			ID: n.ID, Title: n.Title, TitleKey: n.TitleKey,
			Path: n.Path, Icon: n.Icon,
			Hidden:   n.IsHidden == 1,
			Children: adminNavNodes(n.Children),
		})
	}
	return out
}
