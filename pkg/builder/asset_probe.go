package builder

import (
	"go_wp/pkg/builder/core"
)

type assetProbeMemo struct {
	probe func(string) []core.AssetVariant
	cache map[string][]core.AssetVariant
}

func newAssetProbeMemo(probe func(string) []core.AssetVariant) func(string) []core.AssetVariant {
	if probe == nil {
		return nil
	}
	m := &assetProbeMemo{
		probe: probe,
		cache: make(map[string][]core.AssetVariant, 8),
	}
	return m.lookup
}

func (m *assetProbeMemo) lookup(url string) []core.AssetVariant {
	if refs, ok := m.cache[url]; ok {
		return refs
	}
	refs := m.probe(url)
	m.cache[url] = refs
	return refs
}
