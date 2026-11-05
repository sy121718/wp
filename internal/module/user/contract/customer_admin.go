package usercontract

import (
	"context"

	userdto "go_wp/internal/module/user/dto"
)

type CustomerAdminPort interface {
	ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error)

	GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error)

	SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error)

	UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error)

	// BulkSetCustomerStatus 在一个事务里批量改账号状态：任一失败整批回滚。
	BulkSetCustomerStatus(ctx context.Context, req *userdto.CustomerBulkStatusReq) error

	// BulkUnlockCustomers 在一个事务里批量解除锁定。
	BulkUnlockCustomers(ctx context.Context, req *userdto.CustomerBulkUnlockReq) error
}

type CustomerQueryReader interface {
	ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error)

	GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error)
}

type CustomerWriter interface {
	SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error)

	UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error)
}
