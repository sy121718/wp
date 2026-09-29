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
	ErrInvalidParam    = errors.New(pageenums.ErrInvalidParam)
	ErrPageNotFound    = errors.New(pageenums.ErrPageNotFound)
	ErrProjectNotFound = errors.New(pageenums.ErrProjectNotFound)
	// ErrProjectRequired 跨工程扇出入口无法确定工程作用域（DB-009 第三批）。
	// 只用于「工程表读不到 / 一个工程都没有」这类真实异常：正常多工程部署下这些入口
	// 会逐工程设作用域执行，不会走到这里。
	ErrProjectRequired      = errors.New(pageenums.ErrProjectRequired)
	ErrInvalidKind          = errors.New(pageenums.ErrInvalidKind)
	ErrInvalidDocument      = errors.New(pageenums.ErrInvalidDocument)
	ErrInvalidPath          = errors.New(pageenums.ErrInvalidPath)
	ErrDraftVersionConflict = errors.New(pageenums.ErrDraftVersionConflict)
	ErrPathOccupied         = errors.New(pageenums.ErrPathOccupied)
	ErrNoStagedArtifact     = errors.New(pageenums.ErrNoStagedArtifact)
	ErrRollbackTargetMiss   = errors.New(pageenums.ErrRollbackTargetMiss)
	ErrRebuildRequired      = errors.New(pageenums.ErrRebuildRequired)

	// 重定向管理（审计 SEO-025）。ErrRedirectUnavailable 覆盖「装配期未注入路由契约」
	// 这一种明确异常：此时新增/删除重定向只会产生「线上生效但账上没有」的半成品，
	// 宁可显式失败也不静默跳过。
	ErrRedirectUnavailable = errors.New(pageenums.ErrRedirectUnavailable)
	ErrRedirectNotFound    = errors.New(pageenums.ErrRedirectNotFound)
	ErrRedirectOccupied    = errors.New(pageenums.ErrRedirectOccupied)
	ErrRedirectTargetMiss  = errors.New(pageenums.ErrRedirectTargetMiss)
	ErrRedirectLoop        = errors.New(pageenums.ErrRedirectLoop)

	// 定时上下线（PIPE-7）。到点执行失败**不走这些 sentinel**：那条路径的失败要落进
	// page_schedules.last_error（业务 key），而不是抛给某个请求的调用方。
	ErrScheduleNotFound      = errors.New(pageenums.ErrScheduleNotFound)
	ErrScheduleInPast        = errors.New(pageenums.ErrScheduleInPast)
	ErrScheduleActionInvalid = errors.New(pageenums.ErrScheduleActionInvalid)
	ErrScheduleRunning       = errors.New(pageenums.ErrScheduleRunning)
	ErrScheduleOccupied      = errors.New(pageenums.ErrScheduleOccupied)
)
