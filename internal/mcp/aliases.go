package mcp

import (
	"context"

	pkgmcp "go_wp/pkg/mcp"
)

// 通用工具运行机制归 pkg/mcp；本包提供同名薄包装供既有调用方过渡。

type (
	Tool            = pkgmcp.Tool
	Result          = pkgmcp.Result
	Registry        = pkgmcp.Registry
	Runner          = pkgmcp.Runner
	Authorizer      = pkgmcp.Authorizer
	Schema          = pkgmcp.Schema
	ArgsError       = pkgmcp.ArgsError
	PermissionError = pkgmcp.PermissionError
	IdempotencyStore = pkgmcp.IdempotencyStore
)

var (
	NewRegistry = pkgmcp.NewRegistry
	NewRunner   = pkgmcp.NewRunner
	Object      = pkgmcp.Object
	String      = pkgmcp.String
	Boolean     = pkgmcp.Boolean
	Integer     = pkgmcp.Integer
	Array       = pkgmcp.Array
	Enum        = pkgmcp.Enum
	UserIDFrom  = pkgmcp.UserIDFrom
	WithUserID  = pkgmcp.WithUserID
)

func New[Req any](name, title, description, perm string, schema Schema, handler func(context.Context, Req) (Result, error)) Tool {
	return pkgmcp.New(name, title, description, perm, schema, handler)
}

func NewWrite[Req any](name, title, description, perm string, schema Schema, store IdempotencyStore, handler func(context.Context, Req) (Result, error)) Tool {
	return pkgmcp.NewWrite(name, title, description, perm, schema, store, handler)
}
