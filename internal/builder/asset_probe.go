package builder

import (
	mediacontract "go_wp/internal/module/media/contract"
)

type assetProbeMemo struct {
	probe func(string) []mediacontract.VariantRef
	cache map[string][]mediacontract.VariantRef
}

func newAssetProbeMemo(probe func(string) []mediacontract.VariantRef) func(string) []mediacontract.VariantRef {
	if probe == nil {
		return nil
	}
	m := &assetProbeMemo{
		probe: probe,
		cache: make(map[string][]mediacontract.VariantRef, 8),
	}
	return m.lookup
}

func (m *assetProbeMemo) lookup(url string) []mediacontract.VariantRef {
	if refs, ok := m.cache[url]; ok {
		return refs
	}
	refs := m.probe(url)
	m.cache[url] = refs
	return refs
}
