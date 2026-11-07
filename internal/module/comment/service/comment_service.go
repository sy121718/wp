package commentservice

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
)

const errScene = "comment"

type Service struct {
	model *commentmodel.Model

	entityTypes []commentcontract.EntityType
	byType      map[string]commentcontract.EntityType

	policy commentcontract.EntityPolicy

	identityLimiter *submitLimiter
	sourceLimiter   *submitLimiter

	now func() time.Time
}

var (
	_ commentcontract.CommentService = (*Service)(nil)
	_ commentcontract.FragmentPort   = (*Service)(nil)
)

func NewService(db *commentmodel.Model, entityTypes []commentcontract.EntityType) *Service {
	s := &Service{
		model:           db,
		entityTypes:     append([]commentcontract.EntityType(nil), entityTypes...),
		byType:          make(map[string]commentcontract.EntityType, len(entityTypes)),
		identityLimiter: newLimiter(submitPerIdentity, submitWindow),
		sourceLimiter:   newLimiter(submitPerSource, submitWindow),
		now:             time.Now,
	}
	for _, et := range s.entityTypes {
		s.byType[et.Type] = et
	}
	return s
}

func (s *Service) SetEntityPolicy(p commentcontract.EntityPolicy) { s.policy = p }

func (s *Service) SetTimeSource(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) IsRegisteredEntityType(entityType string) bool {
	_, ok := s.byType[strings.TrimSpace(entityType)]
	return ok
}

func (s *Service) EntityTypeLabels(tr func(key, fallback string) string) []commentdto.EntityType {
	out := make([]commentdto.EntityType, 0, len(s.entityTypes))
	for _, et := range s.entityTypes {
		label := et.Label.Fallback
		if tr != nil {
			label = tr(et.Label.Key, et.Label.Fallback)
		}
		out = append(out, commentdto.EntityType{Type: et.Type, Key: et.Label.Key, Label: label})
	}
	return out
}

func (s *Service) validateProject(projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || len([]rune(projectID)) > 64 {
		return errParam(commentenums.ErrProjectRequired)
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return errParam(commentenums.ErrProjectRequired)
	}
	return nil
}

func (s *Service) validateTarget(projectID, entityType, entityID string) error {
	if err := s.validateProject(projectID); err != nil {
		return err
	}
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return errParam(commentenums.ErrEntityTypeUnknown)
	}
	if len(entityType) > commentenums.MaxEntityTypeLen || !s.IsRegisteredEntityType(entityType) {
		return errParam(commentenums.ErrEntityTypeUnknown)
	}
	if !isSafeEntityID(entityID) {
		return errParam(commentenums.ErrEntityIDInvalid)
	}
	return nil
}

func isSafeEntityID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > commentenums.MaxEntityIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func normalizeBody(body string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, body)
	cleaned = strings.TrimSpace(cleaned)
	if utf8.RuneCountInString(cleaned) < commentenums.MinBodyLen {
		return "", errParam(commentenums.ErrBodyRequired)
	}
	if utf8.RuneCountInString(cleaned) > commentenums.MaxBodyLen {
		return "", errParam(commentenums.ErrBodyTooLong)
	}
	return cleaned, nil
}

func normalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = commentenums.DefaultPageSize
	}
	if pageSize > commentenums.MaxPageSize {
		pageSize = commentenums.MaxPageSize
	}
	return page, pageSize
}

func errParam(key string) error { return commentenums.Err(key) }
