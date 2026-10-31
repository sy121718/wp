package navigationenums

// navigation_enums_source.go — 「菜单位置读取失败」的错误声明（译文共享来源索引，BE-source）。
//
// 为什么单独一个文件、而不是塞进 navigation_enums.go：那个文件的 errFallbacks 与
// 常量是既有的一批（兜底对账测试按 AST 扫全包，新增常量会被自动纳入），本批只追加
// **一条**新错误；独立文件让「本批新增了什么」在 diff 里一眼可见，也避免和别的
// owner 在途改动同一段。
//
// 层归属：enums 只声明 key + 中文兜底 + 状态码（facing.WithStatus），不取词、不拼句、
// 不认识任何服务端实现。

import (
	"net/http"

	"go_wp/pkg/facing"
)

// ErrSourceKindFailed 某个菜单位置的原文读取失败（共享来源索引需要**全部四个位置**）。
//
// 为什么必须显式失败而不是静默跳过：导航标签的译文按 (原文 hash, 语境, 语言) 寻址，
// 而「哪些位置出现了这段原文」决定保存后要失效哪些位置的产物。少读一个位置会让
// 写侧少派发那个位置的 stale（表现为「保存成功、某个位置的实例停在旧标签上」），
// 读侧则会把「读不到」伪装成「这里没有菜单」。两者都是静默不收敛。
//
// 用法分档（由调用方选择）：
//   - 读侧工作台：逐位置降级，本错误进该组错误槽、其余位置照常展示；
//   - 写侧事务：整体失败（Require），一条都不写。
//
// 明细（失败的 kind 列举）由 service 用 ErrDetail 拼（ASCII ": " 分隔），
// 因此本词条内不带 %s 占位符。
const ErrSourceKindFailed = "navigation.err.source.kindFailed"

// sourceKindStatus 声明处给状态码：来源读取是服务端端口故障（不是用户参数问题），
// 归 500 —— 出口只调 response.ErrorStatusAuto，不再二次判断。
const sourceKindStatus = http.StatusInternalServerError

// ErrSourceKind 返回「菜单位置读取失败」的业务错误（自带中文兜底与 500 状态码）。
func ErrSourceKind() error {
	return facing.WithStatus(facing.New(ErrSourceKindFailed, errFallbacks[ErrSourceKindFailed]), sourceKindStatus)
}

// ErrSourceKindDetail 带定位信息的同一条错误（detail = 失败位置列举，如 "header_mobile, footer"）。
//
// 明细用 ASCII ": " 拼（pkg/facing 复合形态判据只认这一种），与 ErrDetail 同形；
// 这里复用 ErrDetail 的拼法，保证 errors.Is 仍能命中 ErrSourceKind。
func ErrSourceKindDetail(detail string) error {
	return facing.WithStatus(ErrDetail(ErrSourceKindFailed, detail), sourceKindStatus)
}
