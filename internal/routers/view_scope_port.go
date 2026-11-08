package routers

import (
	jetpkg "go_wp/pkg/view/jet"
)

// wireViewRenderScope 把模板引擎侧的渲染作用域交给 pkg/view 根包。
//
// pkg/view 只定义出口与协议；Jet 的请求取词、失败展开、导航投影都在它的适配层
// （pkg/view/jet）。根包不 import 子包——依赖方向是 pkg/view/jet → pkg/view；
// 应用装配期调一次引擎侧提供的安装入口。
func wireViewRenderScope() {
	jetpkg.InstallRenderScope()
}
