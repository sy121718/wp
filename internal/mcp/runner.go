package mcp

// runner.go — 工具执行器：把「模型要求调用 X」翻译成「在某个账号的权限下执行 X」。
//
// 与注册表的分工：注册表回答「有哪些工具」（装配期定形、运行期只读），
// 执行器回答「这次能不能调、调出来什么」（每个请求一次）。
// 两者分开是因为**权限是逐请求的** —— 把判定塞进注册表会让它持有当前用户，
// 于是它就无法再被多个请求共享。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/permission"
)

// Authorizer 判定某个后台账号是否持有权限点。
//
// 由装配层注入（实现走 Casbin，口径与页面中间件一致，见 builtin.EnforceForUser）——
// 本包不认识 Casbin，也不认识 gin。
type Authorizer func(ctx context.Context, userID int64, perm permission.Perm) (bool, error)

// PermissionError 账号没有调用该工具的权限。
//
// 单独成类型：上层（agent loop）要把这条**回给模型**（「你没有权限查订单」是用户能得到的最好回答），
// 而把「权限系统没起来」那类错误留在日志里。
type PermissionError struct {
	Tool string
	Perm permission.Perm
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("没有调用 %q 的权限", e.Tool)
}

// Runner 执行工具：查表 → 判权限 → 调执行体。
type Runner struct {
	registry  *Registry
	authorize Authorizer
}

// NewRunner 构造执行器。registry 为 nil 时每次执行都报错（装配期接线缺陷，
// 而不是让「没有工具」静默表现成「模型从不调工具」）。
func NewRunner(registry *Registry, authorize Authorizer) *Runner {
	return &Runner{registry: registry, authorize: authorize}
}

// Tools 全部工具声明（供出站请求的 tools 字段与 /mcp 的 tools/list 用）。
func (r *Runner) Tools() []Tool {
	if r.registry == nil {
		return nil
	}
	return r.registry.List()
}

// Run 执行一次工具调用：arguments 是上游给的**原始 JSON 文本**。
//
// 三步的顺序是刻意的：**先查表、再判权限、最后执行**。
// 判权限与执行调换顺序的失败模式是「权限不足时数据已经出库了」——
// 对调用方来说没返回等于没发生，但审计上那已经是一次越权读取。
//
// 权限判定本身出错（enforcer 未初始化 / 策略查询失败）**归为错误而不是放行**：
// fail closed。反过来会把一次启动故障变成所有人的权限被临时放开。
func (r *Runner) Run(ctx context.Context, userID int64, name, arguments string) (Result, error) {
	if r.registry == nil {
		return Result{}, errors.New("mcp: 工具注册表缺失（装配期接线错误）")
	}
	tool, ok := r.registry.Lookup(name)
	if !ok {
		return Result{}, &UnknownToolError{Name: name}
	}
	if r.authorize == nil {
		return Result{}, errors.New("mcp: 权限判定未接入（装配期接线错误）")
	}
	allowed, err := r.authorize(ctx, userID, tool.Permission())
	if err != nil {
		return Result{}, err
	}
	if !allowed {
		return Result{}, &PermissionError{Tool: name, Perm: tool.Permission()}
	}
	// 空 arguments 视作 `{}`：零参数工具合法，而上游对「没参数」的写法不统一
	//（有的给 ""、有的给 "{}"、有的不给这个字段）。
	body := json.RawMessage(strings.TrimSpace(arguments))
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	// 带上调用者身份：写工具里「谁操作的」只能从这里来（见 identity.go）。
	return tool.Invoke(WithUserID(ctx, userID), body)
}
