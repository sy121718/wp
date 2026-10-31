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
}

type CustomerQueryReader interface {
	ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error)

	GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error)
}

type CustomerWriter interface {
	SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error)

	UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error)
}
