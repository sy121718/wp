package core

import "sync"

type FeatureSet struct {
	mu      sync.RWMutex
	attrs   map[string]struct{}
	classes map[string]struct{}
}

func NewFeatureSet() *FeatureSet {
	return &FeatureSet{
		attrs:   make(map[string]struct{}),
		classes: make(map[string]struct{}),
	}
}

func (f *FeatureSet) UseAttr(names ...string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attrs == nil {
		f.attrs = make(map[string]struct{})
	}
	for _, name := range names {
		if name = normalizeFeatureName(name); name != "" {
			f.attrs[name] = struct{}{}
		}
	}
}

func (f *FeatureSet) UseClass(names ...string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.classes == nil {
		f.classes = make(map[string]struct{})
	}
	for _, name := range names {
		if name = normalizeFeatureName(name); name != "" {
			f.classes[name] = struct{}{}
		}
	}
}

func (f *FeatureSet) Attrs() map[string]struct{} { return f.snapshot(true) }

func (f *FeatureSet) Classes() map[string]struct{} { return f.snapshot(false) }

func (f *FeatureSet) snapshot(attrs bool) map[string]struct{} {
	out := map[string]struct{}{}
	if f == nil {
		return out
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	src := f.classes
	if attrs {
		src = f.attrs
	}
	for k := range src {
		out[k] = struct{}{}
	}
	return out
}

func (f *FeatureSet) Len() int {
	if f == nil {
		return 0
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.attrs) + len(f.classes)
}

func normalizeFeatureName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':

		default:
			out = append(out, r)
		}
	}
	return string(out)
}

type ViewFeatureDeclarer interface {
	DeclareFeatures() (attrs, classes []string)
}

func (c *RenderContext) UseAttr(names ...string) {
	if c == nil || c.Features == nil {
		return
	}
	c.Features.UseAttr(names...)
}

func (c *RenderContext) UseClass(names ...string) {
	if c == nil || c.Features == nil {
		return
	}
	c.Features.UseClass(names...)
}
