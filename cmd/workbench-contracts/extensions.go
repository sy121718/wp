package main

// 应用业务组件注册：生成契约必须来自完整应用注册后的目录（与 routers 装配同一批扩展），
// 不能只生成工具内置组件，否则组件库/对齐规则会在业务组件上缺失。
// 本文件只做登记，不复制组件实现，也不形成第二份生成源。
import (
	_ "go_wp/internal/builder/extensions/addtocart"
	_ "go_wp/internal/builder/extensions/articlelist"
	_ "go_wp/internal/builder/extensions/cardstack"
	_ "go_wp/internal/builder/extensions/carticon"
	_ "go_wp/internal/builder/extensions/checkoutform"
	_ "go_wp/internal/builder/extensions/nav"
	_ "go_wp/internal/builder/extensions/orderlist"
	_ "go_wp/internal/builder/extensions/product"
	_ "go_wp/internal/builder/extensions/productcard"
	_ "go_wp/internal/builder/extensions/productlist"
	_ "go_wp/internal/builder/extensions/productselector"
	_ "go_wp/internal/builder/extensions/searchresults"
	_ "go_wp/internal/builder/extensions/userforms"
)
