package blockcontract

import (
	"context"

	json "go_wp/pkg/jsonx"
)

// PresetResolver 按确定的受控预设版本返回文档副本；不接受客户端提供的业务AST。
// 契约不依赖builder/core，具体注册表由装配层注入。
type PresetResolver interface {
	ResolvePreset(ctx context.Context, key string, version int) (json.RawMessage, error)
}
