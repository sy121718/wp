package navigationservice

import (
	"context"
	"strings"

	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

type navigationRowsModelTx struct {
	m *navigationmodel.Model
}

func (r navigationRowsModelTx) ListForKindTx(ctx context.Context, tx *gorm.DB, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	return r.m.ListTx(ctx, tx, projectID, kind)
}

var (
	_ sourceRowsTxPort = navigationRowsModelTx{}
	_ rowSource        = navigationRowsModelTx{}
)

func NewNavigationRowsTx(m *navigationmodel.Model) RowSourceTx { return navigationRowsModelTx{m: m} }

type sourceRowsTxPort interface {
	ListForKindTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (rows []*navigationmodel.NavigationEntity, err error)
}

func (s *Service) OpenSourceIndexTx(ctx context.Context, tx *gorm.DB, projectID string) (navigationcontract.SourceIndex, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if s == nil || tx == nil {

		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	if s.rowsTx == nil {

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
		collected, err := s.collectKindSourcesTx(ctx, tx, projectID, kind, contextName)
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

func (s *Service) collectKindSourcesTx(ctx context.Context, tx *gorm.DB, projectID, kind, contextName string) ([]navigationcontract.TranslationSourceEntry, error) {
	nodes, err := s.sourceNodesStrictTx(ctx, tx, projectID, kind)
	if err != nil {
		return nil, err
	}

	return kindEntriesOf(ctx, nodes, kind, contextName)
}

func (s *Service) sourceNodesStrictTx(ctx context.Context, tx *gorm.DB, projectID, kind string) ([]*navigationdto.NavigationNode, error) {
	rows := s.rowsTx
	if rows == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	list, err := rows.ListForKindTx(ctx, tx, projectID, kind)
	if err != nil {
		return nil, err
	}
	nodes := navNodesOfRows(list)
	if s.sources == nil {
		return nodes, nil
	}
	if rerr := s.resolveSourceTitlesStrict(ctx, projectID, nodes); rerr != nil {
		return nil, rerr
	}
	return nodes, nil
}
