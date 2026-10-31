package navigationservice

import (
	"context"
	"sort"
	"strings"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

var navigationSourceKinds = []string{kindHeader, kindHeaderMobile, kindFooter, kindFooterMobile}

func navigationLabelContext() string { return i18n.ContentContext("navigation", "label") }

type sourceIndex struct {
	entries  []navigationcontract.TranslationSourceEntry
	byHash   map[string]int
	kinds    []string
	failures []navigationcontract.KindReadError

	nodes map[string][]navigationcontract.NavigationNodeRef
}

func (s *Service) sourceProvider() sourceRows {
	if s == nil {
		return nil
	}
	if s.sourceRowsPort != nil {
		return s.sourceRowsPort
	}
	if s.m == nil {
		return nil
	}
	return navigationRowsModel{m: s.m}
}

var _ navigationcontract.SourceIndex = (*sourceIndex)(nil)

func (s *Service) OpenSourceIndex(ctx context.Context, projectID string) (navigationcontract.SourceIndex, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if s.sourceProvider() == nil {

		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}

	index := &sourceIndex{
		byHash:   map[string]int{},
		nodes:    map[string][]navigationcontract.NavigationNodeRef{},
		kinds:    []string{},
		failures: []navigationcontract.KindReadError{},
	}
	contextName := navigationLabelContext()
	for _, kind := range navigationSourceKinds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		collected, err := s.collectKindSources(ctx, projectID, kind, contextName)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {

				return nil, cerr
			}
			sourceKindFailure(ctx, projectID, kind, err)
			index.failures = append(index.failures, navigationcontract.KindReadError{
				Kind: kind, Err: navigationenums.ErrSourceKindDetail(kind),
			})

			continue
		}
		index.kinds = append(index.kinds, kind)
		for _, entry := range collected {
			index.merge(ctx, entry, kind, projectID)
			if entry.Hash != "" {

				index.nodes[entry.Hash] = append(index.nodes[entry.Hash], navigationcontract.NavigationNodeRef{
					ID: entry.NodeID, Kind: kind, Path: entry.Path,
				})
			}
		}
	}
	if len(index.kinds) == 0 {

		return nil, index.Require()
	}
	index.sortEntries()
	return index, nil
}

func (s *Service) collectKindSources(ctx context.Context, projectID, kind, contextName string) ([]navigationcontract.TranslationSourceEntry, error) {
	nodes, err := s.sourceNodesStrict(ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	return kindEntriesOf(ctx, nodes, kind, contextName)
}

func kindEntriesOf(ctx context.Context, nodes []*navigationdto.NavigationNode, kind, contextName string) ([]navigationcontract.TranslationSourceEntry, error) {
	entries := make([]navigationcontract.TranslationSourceEntry, 0, len(nodes))
	walkNavSources(nodes, func(node *navigationdto.NavigationNode) {
		title := strings.TrimSpace(node.Title)

		entries = append(entries, navigationcontract.TranslationSourceEntry{
			Hash: i18n.ContentHash(title), Source: title, Context: contextName, Kinds: []string{kind},

			NodeID: node.ID, Path: strings.TrimSpace(node.Path),
		})
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Service) sourceNodesStrict(ctx context.Context, projectID, kind string) ([]*navigationdto.NavigationNode, error) {
	rows := s.sourceProvider()
	if rows == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	list, err := rows.ListForKind(ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	nodes := navNodesOfRows(list)
	if s.sources == nil {

		return nodes, nil
	}
	if err := s.resolveSourceTitlesStrict(ctx, projectID, nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *Service) resolveSourceTitlesStrict(ctx context.Context, projectID string, nodes []*navigationdto.NavigationNode) error {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if needsSourceResolution(n) {
			title, url, err := s.sources.ResolveSource(ctx, projectID, n.SourceType, *n.SourceID)
			if err != nil {
				return err
			}
			if title != "" {
				n.Title = title
			}
			if url != "" {
				n.Path = url
			}
		}
		if err := s.resolveSourceTitlesStrict(ctx, projectID, n.Children); err != nil {
			return err
		}
	}
	return nil
}

func needsSourceResolution(n *navigationdto.NavigationNode) bool {
	return n.SourceType != sourceCustom && n.SourceID != nil && *n.SourceID != ""
}

func (s *sourceIndex) merge(ctx context.Context, entry navigationcontract.TranslationSourceEntry, kind, projectID string) {
	idx, ok := s.byHash[entry.Hash]
	if !ok {
		s.byHash[entry.Hash] = len(s.entries)
		s.entries = append(s.entries, entry)
		return
	}
	existing := &s.entries[idx]
	if existing.Source != entry.Source || existing.Context != entry.Context {
		logger.SceneCtx("navigation", ctx).With("project", projectID).With("kind", kind).
			With("hash", entry.Hash).Warn("导航原文哈希冲突：同一指纹对应不同原文，该位置按解析失败处理")

		for _, k := range append(append([]string{}, existing.Kinds...), kind) {
			s.fail(kindSafe(k, kind))
		}
		return
	}
	for _, k := range existing.Kinds {
		if k == kind {
			return
		}
	}
	existing.Kinds = appendKind(existing.Kinds, kind)
}

func kindSafe(k, fallback string) string {
	if strings.TrimSpace(k) == "" {
		return fallback
	}
	return k
}

func (s *sourceIndex) fail(kind string) {
	for _, f := range s.failures {
		if f.Kind == kind {
			return
		}
	}
	s.failures = append(s.failures, navigationcontract.KindReadError{
		Kind: kind, Err: navigationenums.ErrSourceKindDetail(kind),
	})
}

func appendKind(kinds []string, kind string) []string {
	rank := navigationKindRank(kind)
	pos := len(kinds)
	for i, k := range kinds {
		if navigationKindRank(k) > rank {
			pos = i
			break
		}
	}
	kinds = append(kinds, "")
	copy(kinds[pos+1:], kinds[pos:])
	kinds[pos] = kind
	return kinds
}

func navigationKindRank(kind string) int {
	for i, k := range navigationSourceKinds {
		if k == kind {
			return i
		}
	}
	return len(navigationSourceKinds)
}

func (s *sourceIndex) sortEntries() {
	sort.Slice(s.entries, func(i, j int) bool { return s.entries[i].Hash < s.entries[j].Hash })
	s.byHash = make(map[string]int, len(s.entries))
	for i := range s.entries {
		s.byHash[s.entries[i].Hash] = i
		s.entries[i].Kinds = append([]string(nil), s.entries[i].Kinds...)
	}
}

func (s *sourceIndex) Nodes(hash string) []navigationcontract.NavigationNodeRef {
	if s == nil {
		return []navigationcontract.NavigationNodeRef{}
	}
	return append([]navigationcontract.NavigationNodeRef{}, s.nodes[strings.TrimSpace(hash)]...)
}

var _ navigationcontract.SourceIndexNodes = (*sourceIndex)(nil)

func (s *sourceIndex) Locate(hash string) (navigationcontract.TranslationSourceEntry, bool) {
	if s == nil {
		return navigationcontract.TranslationSourceEntry{}, false
	}
	idx, ok := s.byHash[strings.TrimSpace(hash)]
	if !ok {
		return navigationcontract.TranslationSourceEntry{}, false
	}
	return cloneEntry(s.entries[idx]), true
}

func (s *sourceIndex) Entries() []navigationcontract.TranslationSourceEntry {
	if s == nil {
		return []navigationcontract.TranslationSourceEntry{}
	}
	out := make([]navigationcontract.TranslationSourceEntry, 0, len(s.entries))
	for i := range s.entries {
		out = append(out, cloneEntry(s.entries[i]))
	}
	return out
}

func (s *sourceIndex) Kinds() []string {
	if s == nil {
		return []string{}
	}
	return append([]string{}, s.kinds...)
}

func (s *sourceIndex) KindErrors() []navigationcontract.KindReadError {
	if s == nil {
		return []navigationcontract.KindReadError{}
	}
	return append([]navigationcontract.KindReadError{}, s.failures...)
}

func (s *sourceIndex) Require() error {
	if s == nil || len(s.failures) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(s.failures))
	seen := map[string]bool{}
	for _, f := range s.failures {
		if f.Kind == "" || seen[f.Kind] {
			continue
		}
		seen[f.Kind] = true
		kinds = append(kinds, f.Kind)
	}
	if len(kinds) == 0 {
		return navigationenums.ErrSourceKind()
	}
	sort.SliceStable(kinds, func(i, j int) bool {
		return navigationKindRank(kinds[i]) < navigationKindRank(kinds[j])
	})
	return navigationenums.ErrSourceKindDetail(strings.Join(kinds, ", "))
}

func cloneEntry(e navigationcontract.TranslationSourceEntry) navigationcontract.TranslationSourceEntry {
	e.Kinds = append([]string{}, e.Kinds...)
	return e
}

func walkNavSources(nodes []*navigationdto.NavigationNode, visit func(*navigationdto.NavigationNode)) {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		visit(node)
		walkNavSources(node.Children, visit)
	}
}

func sourceKindFailure(ctx context.Context, projectID, kind string, err error) {

	if ctx.Err() != nil {
		return
	}
	logger.SceneCtx("navigation", ctx).With("project", projectID).With("kind", kind).
		Error(err, "读取导航原文失败（该位置不进入共享来源索引）")
}
