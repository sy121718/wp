package aiservice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
)

func (s *Service) ListProviders(ctx context.Context) (res []aidto.Provider, err error) {
	rows, err := s.m.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]aidto.Provider, 0, len(rows))
	for i := range rows {
		out = append(out, s.providerOf(&rows[i]))
	}
	return out, nil
}

func (s *Service) GetProvider(ctx context.Context, id int64) (res *aidto.Provider, err error) {
	if id <= 0 {
		return nil, aienums.Err(aienums.ErrInvalidParam)
	}
	e, err := s.m.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, ErrProviderNotFound
	}
	out := s.providerOf(e)
	return &out, nil
}

func (s *Service) SaveProvider(ctx context.Context, req *aidto.SaveProviderReq) (res *aidto.Provider, err error) {
	if req == nil {
		return nil, aienums.Err(aienums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		return nil, aienums.Err(aienums.ErrDisplayNameRequired)
	}
	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = aienums.ProtocolOpenAIChatCompletions
	}
	if !aienums.IsSupportedProtocol(protocol) {
		return nil, aienums.Err(aienums.ErrProtocolUnsupported)
	}
	baseURL := strings.TrimSpace(req.BaseURL)

	status := aienums.StatusEnabled
	if req.Status != nil {
		status = normalizeStatus(*req.Status)
	}

	if req.ID == 0 {
		key := strings.ToLower(strings.TrimSpace(req.ProviderKey))
		if key == "" {
			return nil, aienums.Err(aienums.ErrProviderKeyRequired)
		}
		dup, derr := s.m.FindByKey(ctx, key)
		if derr != nil {
			return nil, derr
		}
		if dup != nil {
			return nil, ErrProviderKeyExists
		}
		cipherText, cerr := s.encryptSecret(req.APIKey)
		if cerr != nil {
			return nil, cerr
		}
		entity := &aimodel.AIProviderEntity{
			ProviderKey:  key,
			DisplayName:  name,
			BaseURL:      baseURL,
			Protocol:     protocol,
			APIKeyCipher: cipherText,
			Status:       status,
			Sort:         req.Sort,
			ConfigData:   aimodel.JSONMap{},
			Version:      1,
			CreateBy:     req.UpdateBy,
			UpdateBy:     req.UpdateBy,
		}
		if err = s.m.Create(ctx, entity); err != nil {

			if isDuplicateKeyErr(err) {
				return nil, ErrProviderKeyExists
			}
			return nil, err
		}
		out := s.providerOf(entity)
		return &out, nil
	}

	current, err := s.m.FindByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	fields := map[string]any{
		"display_name": name,
		"base_url":     baseURL,
		"protocol":     protocol,
		"status":       status,
		"sort":         req.Sort,
	}
	cipherText, cerr := s.encryptSecret(req.APIKey)
	if cerr != nil {
		return nil, cerr
	}

	baseURLChanged := baseURL != strings.TrimSpace(current.BaseURL)
	if cipherText != "" {
		fields["api_key_cipher"] = cipherText
	} else if baseURLChanged {
		if strings.TrimSpace(current.APIKeyCipher) != "" {
			return nil, ErrAPIKeyRequiredOnBaseURLChange
		}
		fields["api_key_cipher"] = ""
	}
	if err = s.updateWithVersion(ctx, req.ID, req.Version, fields, req.UpdateBy); err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, req.ID)
}

func (s *Service) DeleteProvider(ctx context.Context, id int64) (err error) {
	if id <= 0 {
		return aienums.Err(aienums.ErrInvalidParam)
	}
	rows, derr := s.m.DeleteByID(ctx, id)
	if derr != nil {
		return derr
	}
	if rows == 0 {
		return ErrProviderNotFound
	}
	return nil
}

func (s *Service) SetProviderStatus(ctx context.Context, req *aidto.SetStatusReq) (res *aidto.Provider, err error) {
	if req == nil || req.ID <= 0 {
		return nil, aienums.Err(aienums.ErrInvalidParam)
	}
	current, err := s.m.FindByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	status := normalizeStatus(req.Status)
	if current.Status == status {
		return s.GetProvider(ctx, req.ID)
	}
	if err = s.updateWithVersion(ctx, req.ID, req.Version, map[string]any{"status": status}, req.UpdateBy); err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, req.ID)
}

func normalizeStatus(v int) int {
	if v == aienums.StatusEnabled {
		return aienums.StatusEnabled
	}
	return aienums.StatusDisabled
}

func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}

const (
	tokenPlainPrefix = "wp_"

	tokenRandomBytes = 32

	tokenPrefixLen = 12

	tokenListDefaultLimit = 50

	tokenListMaxLimit = 200
)

type ScopeValidator func(perm string) bool

type TokenIdentity struct {
	TokenID int64
	UserID  int64

	Name   string
	Prefix string

	Scopes []string
}

func (t *TokenIdentity) HasScope(perm string) bool {
	if t == nil || perm == "" {
		return false
	}
	for _, s := range t.Scopes {
		if s == perm {
			return true
		}
	}
	return false
}

type AccessTokenService struct {
	tokens  *aimodel.AccessTokenModel
	scopeOK ScopeValidator
}

func NewAccessTokenService(tokens *aimodel.AccessTokenModel) *AccessTokenService {
	return &AccessTokenService{tokens: tokens}
}

func (s *AccessTokenService) SetScopeValidator(v ScopeValidator) { s.scopeOK = v }

func (s *AccessTokenService) Create(ctx context.Context, req *aidto.TokenCreateReq) (*aidto.TokenCreateResp, error) {
	if req == nil {
		return nil, aienums.Err(aienums.ErrTokenNameRequired)
	}
	if req.UserID <= 0 {

		return nil, aienums.Err(aienums.ErrUserRequired)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, aienums.Err(aienums.ErrTokenNameRequired)
	}
	scopes, err := s.normalizeScopes(req.Scopes)
	if err != nil {
		return nil, err
	}
	expiresAt, err := s.parseExpiry(req.ExpiresAt, time.Now())
	if err != nil {
		return nil, err
	}

	plain, prefix, hash, err := generateToken()
	if err != nil {
		return nil, err
	}
	e := &aimodel.AIAccessTokenEntity{
		Name:        name,
		UserID:      req.UserID,
		TokenPrefix: prefix,
		TokenHash:   hash,
		Scopes:      aimodel.StringList(scopes),
		Status:      int16(aienums.TokenStatusActive),
		ExpiresAt:   expiresAt,
	}
	if err := s.tokens.Insert(ctx, e); err != nil {
		return nil, err
	}
	return &aidto.TokenCreateResp{Token: plain, Item: tokenItemOf(e)}, nil
}

func (s *AccessTokenService) List(ctx context.Context, req *aidto.TokenListReq) ([]aidto.TokenItem, error) {
	if req == nil {
		return nil, aienums.Err(aienums.ErrUserRequired)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = tokenListDefaultLimit
	}
	if limit > tokenListMaxLimit {
		limit = tokenListMaxLimit
	}
	owner := req.UserID
	if req.All {
		owner = 0
	} else if owner <= 0 {
		return nil, aienums.Err(aienums.ErrUserRequired)
	}
	rows, err := s.tokens.List(ctx, owner, limit)
	if err != nil {
		return nil, err
	}
	out := make([]aidto.TokenItem, 0, len(rows))
	for i := range rows {
		out = append(out, tokenItemOf(&rows[i]))
	}
	return out, nil
}

func (s *AccessTokenService) Revoke(ctx context.Context, id int64) error {
	if id <= 0 {
		return aienums.Err(aienums.ErrTokenNotFound)
	}
	n, err := s.tokens.Revoke(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return aienums.Err(aienums.ErrTokenNotFound)
	}
	return nil
}

func (s *AccessTokenService) Verify(ctx context.Context, plain string) (*TokenIdentity, error) {
	invalid := aienums.Err(aienums.ErrTokenInvalid)
	plain = strings.TrimSpace(plain)
	if plain == "" || !strings.HasPrefix(plain, tokenPlainPrefix) {
		return nil, invalid
	}
	row, err := s.tokens.FindByHash(ctx, crypto.Sha256(plain))
	if err != nil {
		return nil, err
	}
	if row == nil || row.Status != int16(aienums.TokenStatusActive) {
		return nil, invalid
	}

	if row.ExpiresAt != nil && !row.ExpiresAt.After(time.Now()) {
		return nil, invalid
	}
	_ = s.tokens.TouchUsed(ctx, row.ID)
	return &TokenIdentity{
		TokenID: row.ID,
		UserID:  row.UserID,
		Name:    row.Name,
		Prefix:  row.TokenPrefix,
		Scopes:  []string(row.Scopes),
	}, nil
}

func (s *AccessTokenService) normalizeScopes(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		p := strings.TrimSpace(item)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		if s.scopeOK == nil || !s.scopeOK(p) {

			return nil, aienums.Err(aienums.ErrTokenScopeUnknown)
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, aienums.Err(aienums.ErrTokenScopeRequired)
	}
	return out, nil
}

func (s *AccessTokenService) parseExpiry(raw string, now time.Time) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	day, err := time.ParseInLocation(utils.LayoutDay, raw, time.UTC)
	if err != nil {
		return nil, aienums.Err(aienums.ErrInvalidParam)
	}
	end := day.AddDate(0, 0, 1)
	if !end.After(now) {

		return nil, aienums.Err(aienums.ErrTokenExpiredInPast)
	}
	return &end, nil
}

func tokenItemOf(e *aimodel.AIAccessTokenEntity) aidto.TokenItem {
	if e == nil {
		return aidto.TokenItem{}
	}
	return aidto.TokenItem{
		ID:           e.ID,
		Name:         e.Name,
		UserID:       e.UserID,
		TokenPrefix:  e.TokenPrefix,
		Scopes:       []string(e.Scopes),
		Status:       e.Status,
		StatusLabel:  aienums.TokenStatusLabel(e.Status),
		ExpiresAt:    utils.NewJSONTimePtr(e.ExpiresAt),
		LastUsedTime: utils.NewJSONTimePtr(e.LastUsedTime),
		RevokedTime:  utils.NewJSONTimePtr(e.RevokedTime),
		CreateTime:   utils.NewJSONTime(e.CreateTime),
	}
}

func generateToken() (plain, prefix, hash string, err error) {
	buf := make([]byte, tokenRandomBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	plain = tokenPlainPrefix + base64.RawURLEncoding.EncodeToString(buf)
	prefix = plain
	if len(prefix) > tokenPrefixLen {
		prefix = prefix[:tokenPrefixLen]
	}
	return plain, prefix, crypto.Sha256(plain), nil
}
