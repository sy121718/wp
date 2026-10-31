package productservice

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/rls"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
)

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

func (s *Service) productWriteScopeTx(ctx context.Context, tx *gorm.DB, projectID string) error {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return rls.ErrInvalidProjectID
	}
	projectID = id.String()
	if err := s.m.LockEntityTx(ctx, tx, "product:write:"+projectID); err != nil {
		return err
	}
	return rls.ScopeTx(tx, projectID)
}

func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if projectID != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", productenums.Err(productenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

func pageArgs(req *productdto.ListReq) utils.Paging {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	return utils.NormalizePaging(inPage, inSize, defaultPageSize, maxPageSize)
}

func optionalPaging(page, size int) (limit, offset int) {
	if page <= 0 && size <= 0 {
		return 0, 0
	}
	paging := utils.NormalizePaging(page, size, defaultPageSize, maxPageSize)
	return paging.Size, paging.Offset
}

func normalizeSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(strings.ReplaceAll(s, " ", "-"))
	if !isLegalSlug(s) {
		return ""
	}
	return s
}

func isLegalSlug(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		case r > 127:
		default:
			return false
		}
	}
	return true
}

func deriveSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func orJSONList(items []string) json.RawMessage {
	if items == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

func orIDList(items []string) json.RawMessage { return orJSONList(items) }

func mediaURL(u string) string { return upload.StorageURL(u) }

func mediaURLs(list []string) []string {
	out := make([]string, 0, len(list))
	for _, u := range list {
		if s := mediaURL(u); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func decodeStrings(raw json.RawMessage) (out []string) {
	out = []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}
