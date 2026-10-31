package productmodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type ProductOwner struct {
	ID        string
	ProjectID string
}

func (m *Model) LookupIDOwners(ctx context.Context, projectIDs, ids []string) (owners map[string]string, err error) {
	owners = map[string]string{}
	scopes := normalizeProjectIDs(projectIDs)
	wanted := normalizeProjectIDs(ids)
	if len(scopes) == 0 || len(wanted) == 0 {
		return owners, nil
	}

	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pid := range scopes {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
			rows := make([]*ProductEntity, 0, len(wanted))
			if ferr := tx.WithContext(ctx).Model(&ProductEntity{}).
				Select(productIDFactColumns).
				Where("project_id = ? AND id IN ?", pid, wanted).
				Find(&rows).Error; ferr != nil {
				return ferr
			}
			for _, r := range rows {
				if r == nil || strings.TrimSpace(r.ProjectID) == "" {
					continue
				}

				if _, ok := owners[r.ID]; !ok {
					owners[r.ID] = r.ProjectID
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return owners, nil
}
