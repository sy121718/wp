package jet

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"
)

// InstallRenderScope 把一个具体的 Jet 实例登记成 pkg/view 根包的渲染作用域。
//
// 为什么由引擎侧提供这一步：根包不能 import 子包（那会把依赖方向倒过来），
// 但应用装配又只愿意写一次。这里由子包反过来提供「装上自己」的入口：
// 应用调一次 InstallRenderScope()，根包的出口就接上真实引擎。
//
// 装配期调用；重复调用是幂等的（覆盖同一组函数值）。
func InstallRenderScope() {
	pageview.SetRenderScope(pageview.RenderScope{
		Attach:       AttachScope,
		Inject:       Inject,
		Translate:    RequestTranslateFunc,
		LangRedirect: LangRedirect,
		RequestURI:   RequestURI,
		BuildFailureData: func(status int, fact facing.Fact, err error) any {
			return BuildFailureData(status, fact, err)
		},
		FactOf:           factOf,
		InternalKey:      InternalKey,
		InternalFallback: InternalFallback,
		NavItemsOf:       func(nodes []pageview.AdminNavNode) any { return NavItemsFor(nodes) },
	})
}

// NavItemsFor 把根包的菜单事实投影成导航项（不做激活 / 展开派生）。
func NavItemsFor(nodes []pageview.AdminNavNode) []NavItem {
	out := make([]NavItem, 0, len(nodes))
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		item := NavItem{ID: n.ID, Title: n.Title, TitleKey: n.TitleKey, Path: n.Path, Icon: n.Icon}
		item.Children = NavItemsFor(n.Children)
		if len(item.Children) == 0 && item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// factOf 是 FactOf 的函数值包装：InstallRenderScope 需要 func(*gin.Context, string, error) facing.Fact。
func factOf(c *gin.Context, scene string, err error) facing.Fact { return FactOf(c, scene, err) }
