package aiservice

// 写操作的「读-改-写」全部落在 model 的 version 条件下（见 updateWithVersion）：
// 两个管理员同时改同一个供应商时，后提交者拿到「已被其他人修改」而不是静默覆盖。

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

// ListProviders 列出全部供应商（按 sort 升序）。
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

// GetProvider 按主键取一个供应商；不存在返回 ErrProviderNotFound。
func (s *Service) GetProvider(ctx context.Context, id int64) (res *aidto.Provider, err error) {
	if id <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
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

// SaveProvider 新建（ID == 0）或按版本号更新。
//
// 两条产品语义落在这里：
//
//	· ProviderKey 只在新建立时生效，更新时**忽略**同名字段 —— 内置默认模型清单按
//	  provider_key 索引（「恢复默认模型」的语义就是「回到这个 key 的内置清单」），
//	  允许改 key 会让一个供应商的目录语义凭空漂移。
//	· APIKey 为空 = 不改动已存的密钥（页面占位文案「已配置 —— 输入新值可替换」）；
//	  非空 = 加密后覆盖。想换密钥就填新值，不填就保持原样。
//	· **改 API 地址即作废旧密钥**（安全补偿控制，见 511 表头注释与 docs/16 的 ADR）：
//	  地址变更时若原本有密钥、这次却没给新值 → 直接拒绝（ErrAPIKeyRequiredOnBaseURLChange，
//	  要求重填）；原本就没密钥 → 写空。旧密钥是「发给这个地址」的凭据，让它跟着新地址
//	  走，等于允许把已存密钥发往请求方自选的任意主机（fetch 的 Authorization 头）。
func (s *Service) SaveProvider(ctx context.Context, req *aidto.SaveProviderReq) (res *aidto.Provider, err error) {
	if req == nil {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		return nil, errors.New(aienums.ErrDisplayNameRequired)
	}
	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = aienums.ProtocolOpenAIChatCompletions
	}
	if !aienums.IsSupportedProtocol(protocol) {
		return nil, errors.New(aienums.ErrProtocolUnsupported)
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	// Status 未表态 = 启用（与 ai_provider.status 的 DEFAULT 1 同一口径）。
	status := aienums.StatusEnabled
	if req.Status != nil {
		status = normalizeStatus(*req.Status)
	}

	if req.ID == 0 {
		key := strings.ToLower(strings.TrimSpace(req.ProviderKey))
		if key == "" {
			return nil, errors.New(aienums.ErrProviderKeyRequired)
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
			// FindByKey 是「先查后插」（TOCTOU），并发提交同一 provider_key 时查不出重复，
			// 只能靠唯一约束兜底 —— 把 PG 23505 / gorm.ErrDuplicatedKey 归口成明确的
			// 业务错误，否则用户看到的是「服务器内部错误」。
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
	// 地址变更即作废旧密钥：旧凭据不能跟着新地址出站（见函数头注释）。
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

// DeleteProvider 删除供应商（模型目录随行一并消失）。
//
// 幂等口径：行不存在返回 ErrProviderNotFound（而不是静默成功）—— 页面上的「删除」
// 是个明确动作，删不掉要说清楚，否则用户会以为删了。
func (s *Service) DeleteProvider(ctx context.Context, id int64) (err error) {
	if id <= 0 {
		return errors.New(aienums.ErrInvalidParam)
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

// SetProviderStatus 启停一个供应商（卡片上的状态点）。
//
// 已是目标状态时直接回读（幂等，不推进版本号）：避免「重复点两下启停」把版本号推高、
// 让同时打开的编辑页无谓冲突。
func (s *Service) SetProviderStatus(ctx context.Context, req *aidto.SetStatusReq) (res *aidto.Provider, err error) {
	if req == nil || req.ID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
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

// normalizeStatus 状态归一：只认 0 / 1，其余值一律按停用处理（fail-closed）。
func normalizeStatus(v int) int {
	if v == aienums.StatusEnabled {
		return aienums.StatusEnabled
	}
	return aienums.StatusDisabled
}

// isDuplicateKeyErr 判「唯一约束冲突」（并发新建同一 provider_key）。
//
// 双判据：优先认 gorm 的翻译错误（连接开启了 TranslateError 时），
// 否则回退到 PG 原始错误文本（SQLSTATE 23505）—— 两种装配下都能归口。
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
