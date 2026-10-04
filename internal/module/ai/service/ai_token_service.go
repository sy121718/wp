// ai_token_service.go — 对外访问令牌（PAT）的签发、校验与撤销。
//
// 这一层只有三件事要做对，其余都是它们的推论：
//
//  1. **明文不落库**：签发时生成 → 返回一次 → 库里只留 SHA-256。
//     因此「忘了令牌」的唯一出路是重新签发，这是设计行为不是缺陷。
//  2. **校验 fail closed**：不存在 / 已撤销 / 已过期 / 明文为空，对外**只有一种说法**
//     （ErrTokenInvalid）—— 区分「不存在」与「已撤销」等于告诉爆破者哪一半猜对了。
//  3. **scope 双向收窄**：令牌能做什么 = 它声明的权限点 ∩ 归属账号本身仍拥有的权限。
//     本层只负责前一半（声明了哪些、这些是否存在）；后一半在校验通过后的调用点判
//     （那里才拿得到 Casbin，见 inbound/http）。
//
// 本层**不 import permission 包**：权限点的存在性通过 ScopeValidator 端口注入
// （装配层接 permission.Known），与本模块其它端口（ChatPort / ToolProvider）同一纪律 ——
// service 不认识 Casbin、也不认识权限点常量表。
package aiservice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
)

const (
	// tokenPlainPrefix 明文令牌的固定前缀（让人一眼看出这是本站令牌，便于泄漏时紧急撤销）。
	tokenPlainPrefix = "wp_"
	// tokenRandomBytes 随机部分的字节数（base64url 后 43 字符，256 位熵）。
	tokenRandomBytes = 32
	// tokenPrefixLen 展示用前缀取多少字符（含固定前缀）。
	tokenPrefixLen = 12
	// tokenListDefaultLimit 列表默认条数。
	tokenListDefaultLimit = 50
	// tokenListMaxLimit 列表条数上限（页面一屏的量级，防手写 limit 拉全表）。
	tokenListMaxLimit = 200
)

// ScopeValidator 判断一个权限点是否已登记（装配层接 permission.Known）。
//
// 未注入时**一律判为未知**（fail closed）：宁可让令牌建不出来，也不要放一把
// scope 里写着不存在权限点的令牌进系统 —— 它的实际能力与申请人的认知不一致。
type ScopeValidator func(perm string) bool

// TokenIdentity 一次成功校验得到的调用者身份。
type TokenIdentity struct {
	// TokenID 令牌行 id（审计用）；UserID 归属账号（权限判定顺着它找人）。
	TokenID int64
	UserID  int64
	// Name 令牌用途备注；Prefix 展示用前缀。
	Name   string
	Prefix string
	// Scopes 该令牌声明的权限点（调用方还要与账号自身权限取交集）。
	Scopes []string
}

// HasScope 判断令牌是否声明了某权限点。
//
// 只查声明，不查账号权限：后者需要 Casbin（调用点才有）。两关都要过，
// 少判一关就变成「令牌写成什么就能干什么」。
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

// AccessTokenService 令牌服务。
type AccessTokenService struct {
	tokens  *aimodel.AccessTokenModel
	scopeOK ScopeValidator
}

// NewAccessTokenService 构造（需要 ai_access_token 表访问器）。
func NewAccessTokenService(tokens *aimodel.AccessTokenModel) *AccessTokenService {
	return &AccessTokenService{tokens: tokens}
}

// SetScopeValidator 注入权限点存在性校验端口（装配层接 permission.Known）。
func (s *AccessTokenService) SetScopeValidator(v ScopeValidator) { s.scopeOK = v }

// Create 签发一把令牌，返回**一次性明文**。
func (s *AccessTokenService) Create(ctx context.Context, req *aidto.TokenCreateReq) (*aidto.TokenCreateResp, error) {
	if req == nil {
		return nil, errors.New(aienums.ErrTokenNameRequired)
	}
	if req.UserID <= 0 {
		// 归属账号必须在：没有它，「谁的令牌在调」就答不出来。
		return nil, errors.New(aienums.ErrUserRequired)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(aienums.ErrTokenNameRequired)
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

// List 取令牌列表。all=true 时看全站，否则只看自己的（权限由路由层判）。
func (s *AccessTokenService) List(ctx context.Context, req *aidto.TokenListReq) ([]aidto.TokenItem, error) {
	if req == nil {
		return nil, errors.New(aienums.ErrUserRequired)
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
		return nil, errors.New(aienums.ErrUserRequired)
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

// Revoke 撤销一把令牌（不删行）。已撤销 / 不存在都回 ErrTokenNotFound ——
// 对调用方来说这两件事没有区别：他要的结果是「这把不能用了」。
func (s *AccessTokenService) Revoke(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New(aienums.ErrTokenNotFound)
	}
	n, err := s.tokens.Revoke(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New(aienums.ErrTokenNotFound)
	}
	return nil
}

// Verify 校验明文令牌，返回调用者身份。
//
// 四种失败（不存在 / 已撤销 / 已过期 / 明文空）**对外只有一种说法**：ErrTokenInvalid。
// 另外这里会顺手记 last_used_time（失败不冒给调用方：旁路观测不该让一次正常调用失败）。
func (s *AccessTokenService) Verify(ctx context.Context, plain string) (*TokenIdentity, error) {
	invalid := errors.New(aienums.ErrTokenInvalid)
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
	// 过期**现算**（不存状态位）：存了就要有任务去翻，而「到点没跑任务就仍然可用」
	// 是这类凭证最不该有的性质。
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

// normalizeScopes 清洗权限点清单：去空白、去重、校验存在性，保序。
//
// 保序是刻意的：scope 会展示在后台，顺序跳来跳去会让人以为内容变了。
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
			// 未注入校验端口时也走这一支（fail closed）：未知权限点必须当场拒。
			return nil, errors.New(aienums.ErrTokenScopeUnknown)
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New(aienums.ErrTokenScopeRequired)
	}
	return out, nil
}

// parseExpiry 解析过期日（yyyy-mm-dd，可空）。语义 = **该日结束**（次日零点失效）。
//
// 与订单区间窗口同一口径（半开区间、UTC 日界）：同一个人算「今天到期的令牌」
// 在两处得出不同结论是最容易埋进去的不一致。
func (s *AccessTokenService) parseExpiry(raw string, now time.Time) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	day, err := time.ParseInLocation(utils.LayoutDay, raw, time.UTC)
	if err != nil {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	end := day.AddDate(0, 0, 1)
	if !end.After(now) {
		// 建一把「一来就失效」的令牌没有意义，当场拒比让它静默不可用好。
		return nil, errors.New(aienums.ErrTokenExpiredInPast)
	}
	return &end, nil
}

// tokenItemOf 实体 → 对外项（**不出哈希**）。
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

// generateToken 生成明文令牌、展示前缀与哈希。
//
// 明文 = "wp_" + base64url(32 随机字节)（无填充，避免 `=` 在 URL/JSON 里被转义出错）。
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
