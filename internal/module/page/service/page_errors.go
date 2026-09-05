package pageservice

import (
	"errors"

	pageenums "go_wp/internal/module/page/enums"
)

// 本包 sentinel error（审计项「page 错误码靠中文文案 strings.Contains 匹配」）。
//
// 修复前：service 各处 errors.New(pageenums.ErrXxx) 生成普通字符串错误，
// handler 用 strings.Contains(err.Error(), 文案) 分类映射 HTTP 状态码——
// 文案改动/拼接前缀即失效，属于脆弱的字符串耦合。
// 修复后：service 统一返回下方包级 sentinel（Error() 文案与 pageenums 一致，
// 前端响应文案不变），handler 通过 errors.Is 精确分类。
//
// pipeline 内核错误（internal/pipeline/publisher.go 已定义 ErrPageNotFound /
// ErrVersionConflict / ErrNoStagedArtifact / ErrRollbackPathMismatch 等 sentinel）
// 由 mapPublishError 归一到本包 sentinel（见 page_publish.go）；
// pipeline 侧尚未 sentinel 化的字符串错误暂以 default 分支原样透传，
// 待 pipeline 后续 sentinel 化后统一 %w 收敛。
var (
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
	ErrInvalidParam         = errors.New(pageenums.ErrInvalidParam)
	ErrPageNotFound         = errors.New(pageenums.ErrPageNotFound)
	ErrProjectNotFound      = errors.New(pageenums.ErrProjectNotFound)
	ErrInvalidKind          = errors.New(pageenums.ErrInvalidKind)
	ErrInvalidDocument      = errors.New(pageenums.ErrInvalidDocument)
	ErrInvalidPath          = errors.New(pageenums.ErrInvalidPath)
	ErrDraftVersionConflict = errors.New(pageenums.ErrDraftVersionConflict)
	ErrPathOccupied         = errors.New(pageenums.ErrPathOccupied)
	ErrNoStagedArtifact     = errors.New(pageenums.ErrNoStagedArtifact)
	ErrRollbackTargetMiss   = errors.New(pageenums.ErrRollbackTargetMiss)
	ErrRebuildRequired      = errors.New(pageenums.ErrRebuildRequired)
)
