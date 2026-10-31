package mcp

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/permission"
)

type Authorizer func(ctx context.Context, userID int64, perm permission.Perm) (bool, error)

type PermissionError struct {
	Tool string
	Perm permission.Perm
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("没有调用 %q 的权限", e.Tool)
}

type Runner struct {
	registry  *Registry
	authorize Authorizer
}

func NewRunner(registry *Registry, authorize Authorizer) *Runner {
	return &Runner{registry: registry, authorize: authorize}
}

func (r *Runner) Tools() []Tool {
	if r.registry == nil {
		return nil
	}
	return r.registry.List()
}

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

	body := json.RawMessage(strings.TrimSpace(arguments))
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}

	return tool.Invoke(WithUserID(ctx, userID), body)
}
