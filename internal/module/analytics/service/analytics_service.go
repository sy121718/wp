package analyticsservice

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticsmodel "go_wp/internal/module/analytics/model"
	projectcontract "go_wp/internal/module/project/contract"
)

var _ analyticscontract.AnalyticsService = (*Service)(nil)

var (
	ErrInvalidParam = analyticsenums.Err(analyticsenums.ErrInvalidParam)
	ErrInvalidRange = analyticsenums.Err(analyticsenums.ErrInvalidRange)
)

type Service struct {
	m *analyticsmodel.Model

	pepper string

	now func() time.Time

	projects projectcontract.ProjectService

	retention projectcontract.RetentionPolicyReader
}

func (s *Service) SetProjects(projects projectcontract.ProjectService) {
	if s == nil || projects == nil {
		return
	}
	s.projects = projects
	s.retention, _ = projects.(projectcontract.RetentionPolicyReader)
}

func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.projects == nil {
		return nil, analyticsenums.Err(analyticsenums.ErrInvalidParam)
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, analyticsenums.Err(analyticsenums.ErrInvalidParam)
	}
	return ids, nil
}

func NewService(m *analyticsmodel.Model, pepper string) *Service {
	return &Service{m: m, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}
}

func ResolveAnonSalt(independent, sessionSecret string) (salt string, independentSalt bool) {
	if v := strings.TrimSpace(independent); v != "" {
		return v, true
	}
	return deriveAnonSalt(sessionSecret), false
}

const anonSaltInfo = "go_wp/analytics/anon-hash/v1"

const anonSaltLen = 32

const anonSaltMinLen = 32

func WeakAnonSalt(salt string) bool {
	return len(strings.TrimSpace(salt)) < anonSaltMinLen
}

func deriveAnonSalt(sessionSecret string) string {
	if sessionSecret == "" {
		return ""
	}
	key, err := hkdf.Key(sha256.New, []byte(sessionSecret), nil, anonSaltInfo, anonSaltLen)
	if err != nil {

		return ""
	}
	return hex.EncodeToString(key)
}

func (s *Service) hashAnon(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s.pepper + "\x00" + raw))
	return hex.EncodeToString(sum[:16])
}
