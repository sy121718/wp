package blockservice

import (
	blockcontract "go_wp/internal/module/block/contract"
)

// 本包 sentinel error：重导出 contract 包的同一实例（审计项「block 错误码靠中文文案
// strings.Contains 匹配」）。
//
// sentinel 定义在 contract 包（错误是跨模块契约的一部分，调用方按 errors.Is 精确分类
// 而无需 import 本 service 包）；本包重导出同一实例，保证 service 内部返回的 error 与
// 调用方 errors.Is 判等一致。Error() 文案与 blockenums 常量一致，前端响应文案不变。
var (
	// ErrParamRequired 请求参数缺失（nil 请求、空/空白 ID、空 projectID 等）。
	ErrParamRequired = blockcontract.ErrParamRequired
	// ErrNotFound 全局块不存在。
	ErrNotFound = blockcontract.ErrNotFound
	// ErrProjectNotFound 工程不存在。
	ErrProjectNotFound = blockcontract.ErrProjectNotFound
	// ErrNameRequired 块名称缺失。
	ErrNameRequired = blockcontract.ErrNameRequired
	// ErrInvalidDoc 块文档不合法（与页面文档同构校验失败）。
	ErrInvalidDoc = blockcontract.ErrInvalidDoc
	// ErrInvalidKind 块类型不合法（非 block/header/footer）。
	ErrInvalidKind = blockcontract.ErrInvalidKind
	// ErrInvalidCategory 块分类不合法（未通过白名单）。
	ErrInvalidCategory = blockcontract.ErrInvalidCategory
	// ErrDuplicate 同工程同名块已存在。
	ErrDuplicate = blockcontract.ErrDuplicate
)
