package viewtest

import (
	"os"
	"testing"

	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"
	jetpkg "go_wp/pkg/view/jet"
)

// TestMain 给本包测试装配真实的 Jet 渲染作用域。
//
// 生产装配见 internal/routers/view_scope_port.go：pkg/view 根包不 import 引擎子包，
// 由应用装配期注入。测试用真实适配层，失败出口行为不是桩出来的。
func TestMain(m *testing.M) {
	pageview.SetRenderScope(pageview.RenderScope{
		Attach:       jetpkg.AttachScope,
		Inject:       jetpkg.Inject,
		Translate:    jetpkg.RequestTranslateFunc,
		LangRedirect: jetpkg.LangRedirect,
		RequestURI:   jetpkg.RequestURI,
		BuildFailureData: func(status int, fact facing.Fact, err error) any {
			return jetpkg.BuildFailureData(status, fact, err)
		},
		FactOf:           jetpkg.FactOf,
		InternalKey:      jetpkg.InternalKey,
		InternalFallback: jetpkg.InternalFallback,
		NavItemsOf:       func(nodes []pageview.AdminNavNode) any { return navItemsOf(nodes) },
	})
	os.Exit(m.Run())
}
