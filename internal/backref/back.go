// Package backref 把**当前请求的查询参数**适配成写操作之后的回跳地址。
//
// 它只做一件事：读本次请求的 query，按调用点显式列出的键交给 pkg/backpath 拼出回跳地址；
// 路径不合法（不是站内相对路径 / 超长）时记一条 Warn 并兜底到控制面首页。
//
// 三处职责的分工（本包只占第二件）：
//   - 拼参规则（去空白、长度上限、控制字符）与路径判据在 pkg/backpath / pkg/safepath ——
//     **纯构造**：不 import gin、不读 *gin.Context、不记日志；
//   - gin 取查询、两条 Warn、`/admin` 兜底在本包 —— 它们是**请求适配**，不是通用工具；
//   - 页面出口（Prepare / RenderJump / 按钮码 / 文案翻译 / 导航 / 权限）**不在**本包，
//     也不随本包搬迁 —— 那些是另一批职责（见 task-50 的只读设计结论）。
//
// 日志 scene 固定 "shell"：scene 决定日志目录名（<base>/shell/app-*.log），改名会让
// 既有的日志检索路径与告警规则失效，属**独立变更** —— 本包迁移不夹带它。
package backref

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/backpath"
	"go_wp/pkg/logger"
)

// adminHomePath 控制面首页（仪表盘）：路径不合法时的兜底落点。
//
// 与 internal/shell、admin 模块里的同值常量**同值但互不依赖**（合并它们是独立变更，
// 本批不做）。不能回落到 "/" —— / 已归前台首页，后台的「回到首页」类回落若指向它，
// 操作者会被丢到店铺首页，看起来像登录态丢了。
const adminHomePath = "/admin"

// logScene 日志场景名：scene 就是日志目录名（<base>/<scene>/app-*.log）。
//
// 固定写 "shell" 是刻意的**兼容**：scene 改名不是搬运的一部分 —— 它会让运维侧既有的
// 检索路径与告警规则静默失效，必须单独批准并同步改运维配置。
const logScene = "shell"

// BackPath 拼写操作后的回跳地址：path + 从**本次请求的 query** 里按 keys 透传的参数。
//
// 典型用法（表单 action 上带上下文，服务端自己读回来）：
//
//	<form method="post" action="/admin/coupons/create?project={{.ProjectID}}&status={{.FilterStatus}}">
//
// 然后 handler 里：
//
//	back := backref.BackPath(c, "/admin/coupons", "project", "status", "keyword", "page", "limit")
//
// 逐条行为（与从 internal/shell 迁出前**逐条等价**，本批只搬位置、不改语义）：
//   - 请求缺失（c / c.Request / c.Request.URL 任一为 nil）→ 直接回 adminHomePath，不记日志；
//   - 路径不合法（非站内相对路径 / 超过 safepath.MaxReturnPathBytes / 含控制字符）→
//     记一条 Warn（带 path 字段）并回 adminHomePath：调用点写的是自己代码里的字面量，
//     非法属于编程错误，但**不能因此把用户打成 500**；
//   - 值级：trim 后为空 → 丢弃且**静默**（既不产出 `?status=&page=` 这种噪音，也不刷日志）；
//     超过 backpath.MaxQueryValueBytes 或含控制字符 → 丢弃并记一条 Warn（带 key / len，
//     len 是 **trim 后**的字节数）；其余按 trim 后的值透传，**值不做语义解释**
//     （是不是合法状态码由页面按老规矩校验）。
//
// 判据只有一份：值级用 backpath.KeepValue（Build 内部用的就是它），路径用 pkg/safepath
// （与语言切换回跳共用同一份）—— 所以「构造时丢掉的」与「日志里说丢掉的」永远是同一批。
func BackPath(c *gin.Context, path string, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	source := c.Request.URL.Query()
	back, ok := backpath.Build(path, source, keys...)
	if !ok {
		logger.Scene(logScene).
			With("path", path).
			Warn("回跳路径不合法（非站内相对路径或超长），回控制面首页")
		return adminHomePath
	}

	for _, k := range keys {
		v := strings.TrimSpace(source.Get(k))
		if v == "" || backpath.KeepValue(v) {
			continue
		}
		logger.Scene(logScene).
			With("key", k).
			With("len", len(v)).
			Warn("回跳参数被丢弃（超长或含控制字符）")
	}
	return back
}
