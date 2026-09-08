// Package blockcontract 定义 block 模块对外契约。
package blockcontract

import (
	"errors"

	blockenums "go_wp/internal/module/block/enums"
)

// 本包 sentinel error：block 模块对外的错误契约。
//
// 错误是跨模块契约的一部分：调用方（如 dashboard）需按 errors.Is 精确分类业务错误，
// 而不得 import block 模块的 service 实现，故 sentinel 定义在 contract 包。
// service 层通过重导出（blockservice 内 `var ErrXxx = blockcontract.ErrXxx`）复用同一实例，
// 保证 errors.Is 判等一致。Error() 文案与 blockenums 常量一致，前端响应文案不变。
var (
	// ErrParamRequired 请求参数缺失（nil 请求、空/空白 ID、空 projectID 等）。
	ErrParamRequired = errors.New(blockenums.ErrBlockParamRequired)
	// ErrNotFound 全局块不存在。
	ErrNotFound = errors.New(blockenums.ErrBlockNotFound)
	// ErrProjectNotFound 工程不存在。
	ErrProjectNotFound = errors.New(blockenums.ErrProjectNotFound)
	// ErrNameRequired 块名称缺失。
	ErrNameRequired = errors.New(blockenums.ErrBlockNameRequired)
	// ErrInvalidDoc 块文档不合法（与页面文档同构校验失败）。
	ErrInvalidDoc = errors.New(blockenums.ErrBlockInvalidDoc)
	// ErrInvalidKind 块类型不合法（非 block/header/footer）。
	ErrInvalidKind = errors.New(blockenums.ErrBlockInvalidKind)
	// ErrInvalidCategory 块分类不合法（未通过白名单）。
	ErrInvalidCategory = errors.New(blockenums.ErrBlockInvalidCategory)
	// ErrDuplicate 同工程同名块已存在。
	ErrDuplicate = errors.New(blockenums.ErrBlockDuplicate)
)
