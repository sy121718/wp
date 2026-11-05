package publish

import (
	"context"
	"sort"
	"sync"

	"go_wp/pkg/logger"
)

const (
	DepKindDirectContent = "direct_content"

	DepKindContentCollection = "content_collection"

	DepKindContentTemplate = "content_template"

	DepKindNavigation = "navigation"

	DepKindMenu = "menu"

	DepKindMedia = "media"

	DepKindBlock = "block"

	DepKindPageTarget         = "page_target"
	DepKindPresentationTarget = "presentation_target"

	DepKindSiteSetting = "site_setting"

	DepKindI18N = "i18n"

	DepKindRuntime = "runtime"
)

type DepKey struct {
	Kind string
	Key  string
}

func DirectContentKey(entityType, entityID string) DepKey {
	return DepKey{Kind: DepKindDirectContent, Key: entityType + ":" + entityID}
}

func ContentCollectionKey(entityType string) DepKey {
	return DepKey{Kind: DepKindContentCollection, Key: "collection:content:" + entityType}
}

func ContentTemplateKey(templateID string) DepKey {
	return DepKey{Kind: DepKindContentTemplate, Key: "content_template:" + templateID}
}

func BlockKey(blockID string) DepKey {
	return DepKey{Kind: DepKindBlock, Key: "block:" + blockID}
}

func MenuKey(projectID, kind string) DepKey {
	return DepKey{Kind: DepKindMenu, Key: "menu:" + projectID + ":" + kind}
}

func SiteSettingKey(projectID, key string) DepKey {
	return DepKey{Kind: DepKindSiteSetting, Key: "site_setting:" + projectID + ":" + key}
}

func NavigationKey(navigationID string) DepKey {
	return DepKey{Kind: DepKindNavigation, Key: "navigation:" + navigationID}
}

type DependencyTarget interface {
	MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error)
}

type targetEntry struct {
	sourceType string
	target     DependencyTarget
}

type StaleRebuilder interface {
	RebuildStale(ctx context.Context, ids []string) error
}

type Fanout struct {
	mu          sync.RWMutex
	targets     []targetEntry
	rebuilders  map[string]StaleRebuilder
	syncRebuild bool
}

func NewFanout() *Fanout {
	return &Fanout{rebuilders: map[string]StaleRebuilder{}}
}

func (f *Fanout) Register(sourceType string, t DependencyTarget) {
	if f == nil || t == nil || sourceType == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = append(f.targets, targetEntry{sourceType: sourceType, target: t})
}

func (f *Fanout) RegisteredSourceTypes() []string {
	if f == nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]string, 0, len(f.targets))
	for _, e := range f.targets {
		out = append(out, e.sourceType)
	}
	return out
}

func (f *Fanout) SetSyncRebuild(v bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncRebuild = v
}

func (f *Fanout) SetRebuilder(sourceType string, r StaleRebuilder) {
	if f == nil || sourceType == "" || r == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rebuilders == nil {
		f.rebuilders = map[string]StaleRebuilder{}
	}
	f.rebuilders[sourceType] = r
}

func (f *Fanout) Invalidate(ctx context.Context, kind, key string) {
	f.InvalidateKeys(ctx, DepKey{Kind: kind, Key: key})
}

func (f *Fanout) InvalidateKeys(ctx context.Context, keys ...DepKey) map[string][]string {
	affected := map[string][]string{}
	if f == nil {
		return affected
	}
	f.mu.RLock()
	targets := append([]targetEntry(nil), f.targets...)
	rebuilders := make(map[string]StaleRebuilder, len(f.rebuilders))
	for k, v := range f.rebuilders {
		rebuilders[k] = v
	}
	syncRebuild := f.syncRebuild
	f.mu.RUnlock()

	seen := map[string]map[string]bool{}
	for _, key := range keys {
		if key.Kind == "" || key.Key == "" || key.Kind == DepKindRuntime {
			continue
		}
		for _, entry := range targets {
			ids, err := entry.target.MarkStaleByDependency(ctx, key.Kind, key.Key)
			if err != nil {
				logger.Scene("dependency").With("kind", key.Kind).With("key", key.Key).
					With("source", entry.sourceType).Error(err, "依赖失效标记失败（已降级，不影响内容写入）")
				continue
			}
			if len(ids) == 0 {
				continue
			}
			st := entry.sourceType
			if seen[st] == nil {
				seen[st] = map[string]bool{}
			}
			for _, id := range ids {
				if id == "" || seen[st][id] {
					continue
				}
				seen[st][id] = true
				affected[st] = append(affected[st], id)
			}
		}
	}

	for st, ids := range affected {
		sort.Strings(ids)
		r := rebuilders[st]
		if r == nil {
			continue
		}
		rebuildIDs := append([]string(nil), ids...)
		rebuilder := r
		sourceType := st
		runRebuild := func() {
			bg := context.Background()
			if err := rebuilder.RebuildStale(bg, rebuildIDs); err != nil {
				logger.Scene("dependency").With("source", sourceType).With("count", len(rebuildIDs)).
					Error(err, "依赖失效后的自动重建失败（已标记 stale，等待人工/下次触发）")
			}
		}
		if syncRebuild {
			runRebuild()
		} else {

			go runRebuild()
		}
	}
	return affected
}
