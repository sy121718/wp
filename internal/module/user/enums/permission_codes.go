package userenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	UserCustomerBulkStatus = "user:customer_bulk_status"
	UserCustomerBulkUnlock = "user:customer_bulk_unlock"
	UserCustomerDetail = "user:customer_detail"
	UserCustomerList = "user:customer_list"
	UserCustomerStatus = "user:customer_status"
	UserCustomerUnlock = "user:customer_unlock"
)
