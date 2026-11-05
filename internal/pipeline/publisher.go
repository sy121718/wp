package pipeline

import (
	"context"

	"go_wp/internal/templates"
	"go_wp/pkg/builder"
	"go_wp/pkg/publish"
)

// 应用侧的发布装配：把构建器与页面文档解析注入中立发布内核。
//
// 发布内核（pkg/publish）不认识构建器与模板；这里提供它需要的两件事：
// 默认编译函数与页面访问守卫解析。

// DefaultCompile 用构建器把页面文档编译成发布 HTML。
func DefaultCompile(ctx context.Context, in publish.BuildInput) ([]byte, error) {
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		return nil, err
	}

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, err
	}
	opts := append(ClientAssetOptions(),
		builder.WithContext(ctx), builder.WithCompileMode(builder.CompileModePublish),
		builder.WithComponentSet(set), builder.WithLanguage(in.Lang))
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		return nil, err
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

// PageGuardFunc 是应用侧的页面守卫解析实现（页面文档 → 守卫文件 + 访问级别）。
func PageGuardFunc(docJSON []byte, lang string) (map[string][]byte, *publish.ManifestAccess, error) {
	entries, access, err := PageGuardEntries(docJSON, lang)
	if err != nil {
		return nil, nil, err
	}
	return entries, access, nil
}

// NewPublisher 装配发布内核：注入应用编译与守卫解析。
func NewPublisher(store publish.Store, pub publish.PublicationStore, opts ...publish.Option) *publish.Publisher {
	return publish.NewPublisher(store, pub, PageGuardFunc, opts...)
}

// 以下类型别名供应用侧继续使用短名字；实现都在 pkg/publish。
type (
	Publisher          = publish.Publisher
	BuildInput         = publish.BuildInput
	CompileFn          = publish.CompileFn
	CompileUsage       = publish.CompileUsage
	Draft              = publish.Draft
	Option             = publish.Option
	PageRecord         = publish.PageRecord
	HistoryEntry       = publish.HistoryEntry
	DependencyProvider = publish.DependencyProvider
)

// 状态、错误与动作目标：发布内核的正式名字，应用侧保留短别名。
const (
	StateDraft      = publish.StateDraft
	StateBuilding   = publish.StateBuilding
	StateReady      = publish.StateReady
	StateFailed     = publish.StateFailed
	StatePublished  = publish.StatePublished
	StateSuperseded = publish.StateSuperseded
)

var (
	ErrPageNotFound         = publish.ErrPageNotFound
	ErrVersionConflict      = publish.ErrVersionConflict
	ErrNoStagedArtifact     = publish.ErrNoStagedArtifact
	ErrRollbackPathMismatch = publish.ErrRollbackPathMismatch
)

var (
	WithCompile              = publish.WithCompile
	WithDependencies         = publish.WithDependencies
	WithCompileUsage         = publish.WithCompileUsage
	CompileUsageFromContext  = publish.CompileUsageFromContext
	ActionTargetDependencies = publish.ActionTargetDependencies
)
