package builder

import (
	"bytes"
	"cmp"
	json "go_wp/pkg/jsonx"
	"slices"
	"sort"
	"strings"

	"go_wp/pkg/builder/components/globalref"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

type ContentCandidate struct {
	Context string
	Source  string
}

const (
	SEOTitleContext       = "page.seo.title"
	SEODescriptionContext = "page.seo.description"
)

func CollectContentCandidates(p *Page) []ContentCandidate {
	if p == nil {
		return nil
	}
	return CollectContentCandidatesOfRoots(p.Root)
}

func CollectContentCandidatesOfRoots(roots []*core.Node) []ContentCandidate {
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []ContentCandidate
	for _, n := range roots {
		collectNodeCandidates(n, seen, &out)
	}
	sortCandidates(out)
	return out
}

type BlockRootFunc func(blockID string) ([]*core.Node, error)

func CollectContentCandidatesForDocument(p *Page, resolve BlockRootFunc) []ContentCandidate {
	if p == nil {
		return nil
	}

	bindings := p.Settings.Structure.SlotBindings()
	extra := make([]string, 0, len(bindings))
	for _, slot := range SortedSlots(bindings) {
		extra = append(extra, bindings[slot])
	}

	templateBindings := p.Settings.Structure.TemplateBindings()
	for _, slot := range SortedSlots(templateBindings) {
		extra = append(extra, StructureTemplateRef(templateBindings[slot]))
	}
	return AppendSEOCandidates(p, CollectContentCandidatesDeep(p, extra, resolve))
}

func AppendSEOCandidates(p *Page, cands []ContentCandidate) []ContentCandidate {
	if p == nil {
		return cands
	}
	out := make([]ContentCandidate, 0, 2+len(cands))
	if t := strings.TrimSpace(p.Settings.SEO.Title); t != "" {
		out = append(out, ContentCandidate{Context: SEOTitleContext, Source: t})
	}
	if d := strings.TrimSpace(p.Settings.SEO.Description); d != "" {
		out = append(out, ContentCandidate{Context: SEODescriptionContext, Source: d})
	}
	return append(out, cands...)
}

func CollectContentCandidatesDeep(p *Page, extraBlockIDs []string, resolve BlockRootFunc) []ContentCandidate {
	if p == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []ContentCandidate
	for _, n := range p.Root {
		collectNodeCandidates(n, seen, &out)
	}
	visited := map[string]bool{}
	var walk func(roots []*core.Node)
	load := func(blockID string) {
		blockID = strings.TrimSpace(blockID)
		if resolve == nil || blockID == "" || visited[blockID] {
			return
		}
		visited[blockID] = true
		roots, err := resolve(blockID)
		if err != nil || len(roots) == 0 {
			return
		}
		walk(roots)
	}
	walk = func(roots []*core.Node) {
		for _, r := range roots {
			collectNodeCandidates(r, seen, &out)
		}
		for _, id := range ReferencedBlockIDs(roots) {
			load(id)
		}
	}
	for _, id := range ReferencedBlockIDs(p.Root) {
		load(id)
	}
	for _, id := range extraBlockIDs {
		load(id)
	}
	sortCandidates(out)
	return out
}

func ReferencedBlockIDs(roots []*core.Node) []string {
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(n *core.Node)
	walk = func(n *core.Node) {
		if n == nil {
			return
		}
		if n.Type == globalref.Type {
			if id := blockIDOf(n.Props); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	sort.Strings(out)
	return out
}

func blockIDOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var props struct {
		BlockID string `json:"blockId"`
	}
	if err := json.Unmarshal(raw, &props); err != nil {
		return ""
	}
	return strings.TrimSpace(props.BlockID)
}

func sortCandidates(out []ContentCandidate) {
	slices.SortFunc(out, func(a, b ContentCandidate) int {
		return cmp.Or(strings.Compare(a.Context, b.Context), strings.Compare(a.Source, b.Source))
	})
}

func ContentHashes(cands []ContentCandidate) []string {
	if len(cands) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(cands))
	hashes := make([]string, 0, len(cands))
	for _, c := range cands {
		h := i18n.ContentHash(c.Source)
		if seen[h] {
			continue
		}
		seen[h] = true
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	return hashes
}

func collectNodeCandidates(n *core.Node, seen map[string]bool, out *[]ContentCandidate) {
	if n == nil {
		return
	}
	if fields := core.TranslatableFields(n.Type); len(fields) > 0 {
		collectValue(n.Type, "", n.Props, false, fields, seen, out)
	}
	for _, c := range n.Children {
		collectNodeCandidates(c, seen, out)
	}
}

func collectValue(typeName, key string, raw json.RawMessage, whitelisted bool, fields map[string]bool, seen map[string]bool, out *[]ContentCandidate) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return
	}
	switch trimmed[0] {
	case '"':
		if !whitelisted {
			return
		}
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return
		}
		if !i18n.ShouldTranslateContent(s) {
			return
		}
		ctx := i18n.ContentContext(typeName, key)
		if ctx == "" {
			return
		}
		dedupe := ctx + "\x00" + s
		if seen[dedupe] {
			return
		}
		seen[dedupe] = true
		*out = append(*out, ContentCandidate{Context: ctx, Source: s})
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return
		}
		for _, el := range arr {
			t := bytes.TrimSpace(el)
			if len(t) == 0 {
				continue
			}
			if t[0] == '{' {
				collectValue(typeName, key, el, false, fields, seen, out)
				continue
			}
			collectValue(typeName, key, el, whitelisted, fields, seen, out)
		}
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return
		}
		for k, v := range obj {
			collectValue(typeName, k, v, fields[k], fields, seen, out)
		}
	}
}

func applyContentTranslation(n *core.Node, translate func(source, context string) string) *core.Node {
	if n == nil || translate == nil || len(n.Props) == 0 {
		return n
	}
	fields := core.TranslatableFields(n.Type)
	if len(fields) == 0 {
		return n
	}
	newProps, changed := translateProps(n.Type, n.Props, fields, translate)
	if !changed {
		return n
	}
	cp := *n
	cp.Props = newProps
	return &cp
}

func translateProps(typeName string, raw json.RawMessage, fields map[string]bool, translate func(source, context string) string) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return raw, false
	}
	changed := false
	for key, val := range obj {
		nv, c := translateValue(typeName, key, val, fields[key], fields, translate)
		if c {
			obj[key] = nv
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false
	}
	return out, true
}

func translateValue(typeName, key string, raw json.RawMessage, whitelisted bool, fields map[string]bool, translate func(source, context string) string) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, false
	}
	switch trimmed[0] {
	case '"':
		if !whitelisted {
			return raw, false
		}
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return raw, false
		}
		ctx := i18n.ContentContext(typeName, key)
		if ctx == "" {
			return raw, false
		}
		target := translate(s, ctx)
		if target == s || target == "" {
			return raw, false
		}
		encoded, err := json.Marshal(target)
		if err != nil {
			return raw, false
		}
		return encoded, true
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return raw, false
		}
		changed := false
		for i, el := range arr {
			t := bytes.TrimSpace(el)
			if len(t) == 0 {
				continue
			}
			if t[0] == '{' {
				nv, c := translateValue(typeName, key, el, false, fields, translate)
				if c {
					arr[i] = nv
					changed = true
				}
				continue
			}
			nv, c := translateValue(typeName, key, el, whitelisted, fields, translate)
			if c {
				arr[i] = nv
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		out, err := json.Marshal(arr)
		if err != nil {
			return raw, false
		}
		return out, true
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return raw, false
		}
		changed := false
		for k, v := range obj {
			nv, c := translateValue(typeName, k, v, fields[k], fields, translate)
			if c {
				obj[k] = nv
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		out, err := json.Marshal(obj)
		if err != nil {
			return raw, false
		}
		return out, true
	}
	return raw, false
}
