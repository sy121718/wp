package blockcontract

import (
	"context"

	json "go_wp/pkg/jsonx"
)

type PresetResolver interface {
	ResolvePreset(ctx context.Context, key string, version int) (json.RawMessage, error)
}
