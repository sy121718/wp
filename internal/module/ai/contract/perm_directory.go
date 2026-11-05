package aicontract

import "context"

// PermEntry 是权限目录的一行事实；名称、路径、方法与启停都来自数据库。
type PermEntry struct {
	Code   string
	Name   string
	Path   string
	Method string
	Status int
}

// PermDirectory 是 AI 侧所需的权限目录只读端口。
//
// 实现在应用装配处包住 admin 的权限目录查询：AI 不直接读 admin 的表，
// 也不保留代码侧名称表。查询失败必须由调用方按拒绝处理（fail closed）。
type PermDirectory interface {
	// Exists 报告权限点是否为已启用的目录项。
	Exists(ctx context.Context, code string) (bool, error)
	// List 批量取目录项；未命中的码不出现在返回里。
	List(ctx context.Context, codes []string) ([]PermEntry, error)
}
