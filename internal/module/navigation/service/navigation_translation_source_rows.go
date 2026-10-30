package navigationservice

// navigation_translation_source_rows.go — 共享来源索引的**行读取端口**与单测注入点。
//
// 为什么单独一层：本批只允许新增文件（既有 model 一行不动），而索引要能被单测注入
// 「某个位置读失败」。端口只有一个方法，生产实现就是 model.List 的直传
// （零加工、零缓存：Tree 每次也是直接 List）。

import (
	"context"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
)

// sourceRows 某工程某位置的菜单行（消费者侧定义的最小端口，生产走 model.List）。
type sourceRows interface {
	// ListForKind 按工程 + 位置取全部菜单项（sort_order 升序，同序按 id 升序）。
	ListForKind(ctx context.Context, projectID, kind string) (rows []*navigationmodel.NavigationEntity, err error)
}

// navigationRowsModel 生产实现：直接转发 model.List。
type navigationRowsModel struct {
	m *navigationmodel.Model
}

// ListForKind 按工程 + 位置列出菜单行（与 Service.Tree 同一读法）。
func (r navigationRowsModel) ListForKind(ctx context.Context, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	return r.m.List(ctx, projectID, kind)
}

// 编译期护栏：端口在生产实现上可用。
var _ sourceRows = navigationRowsModel{}

// navNodesOfRows 把行装成树（转发既有 buildTree：构建期与管理页同一份装配规则）。
func navNodesOfRows(rows []*navigationmodel.NavigationEntity) []*navigationdto.NavigationNode {
	return buildTree(rows)
}
