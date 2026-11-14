package routers

import (
	"context"

	admincontract "go_wp/internal/module/admin/contract"
	aicontract "go_wp/internal/module/ai/contract"
	"go_wp/pkg/casbin"
)

// casbinPolicies 把 pkg/casbin 的策略查询适配成启动核对用的只读事实。
//
// 它查的是**已加载的策略**（与执行期 Enforce 读同一份），不是数据库二次查询：
// 核对要回答的就是「执行期这条码能不能命中任何策略」。
type casbinPolicies struct{}

func (casbinPolicies) HasPolicies(_ context.Context, code string) (bool, error) {
	return casbin.HasPermissionPolicies(code)
}

// adminPermDirectory 把 admin 的权限目录查询适配成 AI 侧只读端口。
//
// 权限名称与启停状态只来自数据库；装配处不做第二份目录，
// 查询错误原样上抛，由消费方按拒绝处理。
type adminPermDirectory struct {
	svc admincontract.AuthzContextService
}

func newAdminPermDirectory(svc admincontract.AuthzContextService) aicontract.PermDirectory {
	if svc == nil {
		return nil
	}
	return adminPermDirectory{svc: svc}
}

func (d adminPermDirectory) Exists(ctx context.Context, code string) (bool, error) {
	return d.svc.ExistsEnabledCode(ctx, code)
}

func (d adminPermDirectory) ListByPathMethod(ctx context.Context, path, method string) ([]aicontract.PermEntry, error) {
	briefs, err := d.svc.ListByPathMethod(ctx, path, method)
	if err != nil {
		return nil, err
	}
	out := make([]aicontract.PermEntry, 0, len(briefs))
	for _, b := range briefs {
		out = append(out, aicontract.PermEntry{
			Code:   b.PermissionCode,
			Name:   b.PermissionName,
			Path:   b.APIPath,
			Method: b.APIMethod,
			Status: b.Status,
		})
	}
	return out, nil
}

func (d adminPermDirectory) List(ctx context.Context, codes []string) ([]aicontract.PermEntry, error) {
	briefs, err := d.svc.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	out := make([]aicontract.PermEntry, 0, len(briefs))
	for _, b := range briefs {
		out = append(out, aicontract.PermEntry{
			Code:   b.PermissionCode,
			Name:   b.PermissionName,
			Path:   b.APIPath,
			Method: b.APIMethod,
			Status: b.Status,
		})
	}
	return out, nil
}
