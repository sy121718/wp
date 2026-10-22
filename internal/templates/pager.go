package templates

// pager.go — 分页条的渲染值。**算法不在本文件**，全部委托 pkg/pagination.Derived。
//
// 为什么只剩一层包装而不是直接让模板用 pagination.Derived：
//
//	模板里写的是 `pager(.Pagination)` 这个全局函数名（33 个页面 + 片段都在用），
//	改名字要动 155 个模板。这一层是**名字适配器**：模板叫 pager，Go 侧叫 Derive。
//	它不含任何算法 —— 页数、夹紧、区间、槽位、链接都在 pkg/pagination/derive.go，
//	因为那套派生同样要给可视化构建器用（那条路径不经过 gin，也 import 不了本包）。
//
// 口径必须与迁移前逐字一致；验收判据是「渲染字节相同」，由
// internal/templates 的分页渲染用例守（pages=2..24 全组合槽位序列比对）。

import "go_wp/pkg/pagination"

// Pager 是分页条的渲染值（= pkg/pagination.Derived 的别名）。
//
// 用类型别名而不是新结构体：模板读的是**字段名**（pg.Total / pg.Page / pg.Start…），
// 别名让两边字面同一，不会出现「加了字段忘了同步」的漂移。
type Pager = pagination.Derived

// PagerOf 把分页事实补成可渲染的派生值（委派 pkg/pagination.Derive）。
func PagerOf(p pagination.Page) Pager {
	return pagination.Derive(p)
}
