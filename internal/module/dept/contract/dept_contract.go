// Package deptcontract 定义部门模块对外暴露的业务契约接口。
package deptcontract

import (
	"context"

	deptdto "go_wp/internal/module/dept/dto"
)

// DeptService 定义部门模块对外暴露的业务能力。
type DeptService interface {
	Tree(ctx context.Context) ([]deptdto.TreeNode, error)
	Detail(ctx context.Context, req *deptdto.DetailReq) (*deptdto.TreeNode, error)
	// AncestorIDs 返回指定部门的全部上级部门 ID。
	AncestorIDs(ctx context.Context, deptID uint64) ([]uint64, error)
	Create(ctx context.Context, req *deptdto.CreateReq) error
	Update(ctx context.Context, req *deptdto.UpdateReq) error
	Delete(ctx context.Context, req *deptdto.DeleteReq) error
	UserList(ctx context.Context, req *deptdto.UserListReq) (interface{}, error)
	UserSave(ctx context.Context, req *deptdto.UserSaveReq) error
}
