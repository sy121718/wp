package productservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
)

const refGuardRefSamples = 2

const refDetailBudget = 380

const refDetailProjectIDs = 2

func (s *Service) crossProjectRefScope(ctx context.Context) (projectIDs []string, err error) {
	if s.project == nil {
		return nil, errors.New("product: 跨工程引用检查需要工程清单，但 project 契约未注入")
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, p := range list {
		if id := strings.TrimSpace(p.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("product: 跨工程引用检查拿不到任何工程（projects 为空）")
	}
	return ids, nil
}

func (s *Service) crossProjectRefs(ctx context.Context, scan func(projectIDs []string) (*productmodel.CrossProjectRef, error)) (*productmodel.CrossProjectRef, error) {
	scope, err := s.crossProjectRefScope(ctx)
	if err != nil {
		return nil, err
	}
	return scan(scope)
}

func crossProjectRefBlocked(key string, ref *productmodel.CrossProjectRef) error {
	if detail := refGuardDetail(ref); detail != "" {
		return fmt.Errorf("%w: %s", productenums.Err(key), detail)
	}
	return productenums.Err(key)
}

func refGuardDetail(ref *productmodel.CrossProjectRef) string {
	if !ref.Referenced() {
		return ""
	}
	return i18n.ErrorDetail(productenums.DetailRefGuardBlocked,
		"columns", strings.Join(ref.RefColumns, " + "),
		"n", strconv.Itoa(ref.Total),
		"projects", strconv.Itoa(len(ref.ProjectIDs)),
		"refs", refGuardRefs(ref))
}

func refGuardRefs(ref *productmodel.CrossProjectRef) string {
	if len(ref.Products) == 0 {
		return ""
	}
	listed := len(ref.Products)
	if listed > refGuardRefSamples {
		listed = refGuardRefSamples
	}
	parts := make([]string, 0, listed)
	for _, p := range ref.Products[:listed] {
		if sku := strings.TrimSpace(p.SKUCode); sku != "" {
			parts = append(parts, p.ProductID+"("+sku+")@"+p.ProjectID)
			continue
		}
		parts = append(parts, p.ProductID+"@"+p.ProjectID)
	}
	return strings.Join(parts, "、")
}
