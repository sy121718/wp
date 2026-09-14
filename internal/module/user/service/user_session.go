package userservice

// user_session.go — 访客会话（issue #36）。
//
// # 为什么不能复用 admin 的那套会话
//
// pkg/auth 的会话是**单槽位**设计：Redis key 里放的是 userID（`user:session:<userID>`），
// cookie 名固定 `gowp_session`。访客账号直接复用它会有两个必然事故：
//
//  1. **cookie 互相顶掉** —— 后台开着的同时在前台登录一次，两边共用同一个 cookie，
//     后登录的那个把先登录的挤掉，管理员会莫名其妙被踢出后台。
//  2. **Redis key 撞号** —— 两边的 id 空间各自自增（sys_admin.id 与 users.id），
//     admin id=1 与访客 id=1 会指向同一个 key。
//
// 因此本模块用**独立命名空间**：cookie `gowp_user_session`、Redis 前缀 `gwp:userauth:sess:`。
// 共用的只有底层基础设施（Redis 本身）与令牌生成方式，不共用任何 key 与 cookie。
//
// # 三层各放什么
//
//   - cookie（签名）：只放令牌本身，只是「你是谁」的凭据，不含业务字段；
//   - Redis：会话状态，决定「这个请求算不算已登录」—— 每个请求都要判，不能查库；
//   - user_sessions 表：台账（令牌哈希 + 设备信息）—— 只在登录 / 列表 / 撤销时碰。
//
// # Redis key 用令牌哈希而不是明文令牌
//
// key 是 `gwp:userauth:sess:<sha256(令牌)>`。这样台账行里存的哈希**直接**就是 Redis key 的
// 后半段，撤销某台设备时不需要「由行反查令牌」——那种反查要么存明文（等于白做哈希），
// 要么额外维护一张映射表（多一处会不一致的状态）。顺带的好处：Redis 里也不出现明文令牌。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/cache"
	"go_wp/pkg/crypto"
	"go_wp/pkg/logger"

	"golang.org/x/crypto/bcrypt"
)

const (
	// sessionTokenBytes 令牌字节数（hex 编码后 64 字符）。
	sessionTokenBytes = 32

	// sessionRedisPrefix 访客会话的 Redis 前缀。
	//
	// 与 admin 的 `user:session:` **必须**不同：两边 id 空间独立、语义也独立，
	// 共用前缀等于让两套身份体系共享同一个槽位。
	sessionRedisPrefix = "gwp:userauth:sess:"

	// 会话有效期与 admin 同口径。cookie 与 Redis 必须一致，
	// 否则会出现「cookie 还在、Redis 会话已过期」的静默掉线。
	userSessionTTL         = 24 * time.Hour
	userSessionRememberTTL = 7 * 24 * time.Hour

	// 登录失败锁定：连续 5 次锁 30 分钟（与 admin 同口径，改一处要两处一起改）。
	loginFailureLockThreshold = 5
	loginFailureLockDuration  = 30 * time.Minute
)

// UserAuthSession Redis 中保存的访客会话。
//
// 只放当场要用的字段。**它不是用户资料的缓存**：昵称头像改了要让下次请求就看到新值，
// 而不是等用户重新登录。
type UserAuthSession struct {
	Token    string `json:"token"`
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
	// RowID 是 user_sessions 表里的行 id：撤销设备与「哪台是当前设备」按它判定。
	RowID uint64 `json:"row_id"`
	// IssuedAt 会话建立时间（秒）。
	IssuedAt int64 `json:"issued_at"`
}

// SessionMeta 建立会话时由 inbound 采集的请求上下文。
//
// 刻意不从 service 里读 gin.Context：service 不该知道 HTTP 的存在。
// 传结构体也让「这些字段来自请求」在签名上就看得出来（客户端不可伪造）。
type SessionMeta struct {
	IP        string
	UserAgent string
	Location  string
}

// SessionTTLFor 会话有效期（勾选记住我 → 7 天）。
func SessionTTLFor(rememberMe bool) time.Duration {
	if rememberMe {
		return userSessionRememberTTL
	}
	return userSessionTTL
}

// newSessionToken 生成会话令牌。
func newSessionToken() (token string, err error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// tokenHash 令牌的 SHA-256（台账里存它、Redis key 用它）。
func tokenHash(token string) string { return crypto.Sha256(token) }

// sessionKey Redis key。入参是**明文令牌**，内部转哈希。
func sessionKey(token string) string {
	return sessionRedisPrefix + tokenHash(strings.TrimSpace(token))
}

// sessionKeyByHash 已知令牌哈希时的 key（撤销路径用，省掉一次哈希）。
func sessionKeyByHash(hash string) string { return sessionRedisPrefix + hash }

// Login 密码登录：校验凭据 → 建会话（台账 + Redis）→ 返回令牌。
//
// 两处顺序不能调换：
//   - **锁定判断在密码校验之前**：反过来锁定期间仍能一遍遍试密码，
//     锁定就只剩「不返回成功」的表面作用，暴力破解照旧。
//   - **失败计数在返回错误之前**：任何提前 return 都必须先记账，
//     否则那条错误路径就是一条免费的爆破通道。
func (s *Service) Login(ctx context.Context, req *userdto.LoginReq, meta SessionMeta) (res *userdto.LoginResp, err error) {
	if s.sm == nil {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	if req == nil {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	account := strings.TrimSpace(req.Account)
	password := req.Password
	if account == "" || password == "" {
		return nil, errors.New(userenums.ErrLoginRequired)
	}

	// 账号可以是用户名或邮箱 —— 两者都大小写不敏感（与迁移 123 的唯一索引同口径）。
	user, ferr := s.m.GetByUsername(ctx, account)
	if ferr != nil {
		user, ferr = s.m.GetByEmail(ctx, account)
	}
	if ferr != nil {
		// 用户不存在：与密码错误返回**同一个**错误（否则登录接口就是账号枚举器）。
		// 这里也**不记**失败计数 —— 没有行可记，而「能记」本身就等于确认了账号存在。
		return nil, errors.New(userenums.ErrBadCredentials)
	}

	// 锁定中：即使密码正确也不放行（锁定的语义就是「这段时间不许登录」）。
	if user.LockedUntilTime != nil && time.Now().Before(*user.LockedUntilTime) {
		remain := time.Until(*user.LockedUntilTime).Round(time.Minute)
		if remain < time.Minute {
			remain = time.Minute
		}
		return nil, fmt.Errorf(userenums.ErrAccountLocked, remain.String())
	}

	// 待激活与禁用分开说 ——「去完成邮箱验证」与「账号被禁用」对用户是两件完全不同的事，
	// 笼统回一句「登录失败」会让人无从下手。
	if user.Status == usermodel.UserStatusPending {
		return nil, errors.New(userenums.ErrAccountPending)
	}
	if user.Status != usermodel.UserStatusActive {
		return nil, errors.New(userenums.ErrAccountDisabled)
	}

	// 空密码 = 第三方注册的账号：它没有本站密码，密码登录必须整体拒绝。
	// 少了这一条，bcrypt 会拿空串去比对哈希，结果虽然也是「失败」，
	// 但对外表现为「密码错误」，那类用户会一直在改密码。
	if strings.TrimSpace(user.Password) == "" {
		return nil, errors.New(userenums.ErrPasswordLoginUnavailable)
	}

	if cerr := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); cerr != nil {
		if uerr := s.m.IncrLoginFailure(ctx, user.ID, loginFailureLockThreshold, loginFailureLockDuration); uerr != nil {
			// 记账失败不能吞掉：吞掉之后锁定永远不会触发，而日志里看不出任何异常。
			logger.Scene("user").Error(uerr, "记录登录失败状态失败")
		}
		return nil, errors.New(userenums.ErrBadCredentials)
	}

	// 凭据正确：清零失败状态、记录登录痕迹、建会话。
	if uerr := s.m.ResetLoginFailure(ctx, user.ID); uerr != nil {
		return nil, uerr
	}
	now := time.Now()
	if uerr := s.m.RecordLogin(ctx, user.ID, meta.IP, meta.Location, now); uerr != nil {
		return nil, uerr
	}

	token, terr := newSessionToken()
	if terr != nil {
		return nil, terr
	}
	ttl := SessionTTLFor(req.RememberMe)

	// 先落台账（DB 是权威记录），再写 Redis（快路径）。
	// 顺序反过来会出现「Redis 里有会话、台账里查不到」——用户显示在线，
	// 设备列表里却找不到它，于是永远踢不掉。
	row := &usermodel.UserSessionEntity{
		UserID:       user.ID,
		TokenHash:    tokenHash(token),
		UserAgent:    optString(meta.UserAgent),
		IP:           optString(meta.IP),
		Location:     optString(meta.Location),
		LastActiveAt: &now,
	}
	if uerr := s.sm.Create(ctx, row); uerr != nil {
		return nil, uerr
	}

	sess := &UserAuthSession{
		Token:    token,
		UserID:   user.ID,
		Username: user.Username,
		Nickname: displayName(user),
		Avatar:   deref(user.Avatar),
		Email:    user.Email,
		RowID:    row.ID,
		IssuedAt: now.Unix(),
	}
	if uerr := cache.SetJSON(ctx, sessionKey(token), sess, ttl); uerr != nil {
		// Redis 写不进去 → 会话并不存在：把台账行撤销掉，
		// 不留一条「查得到却用不了」的记录（那会变成用户在设备列表里看到一个假设备）。
		if _, rerr := s.sm.RevokeByTokenHash(ctx, tokenHash(token), now); rerr != nil {
			logger.Scene("user").Error(rerr, "登录失败回滚会话台账失败")
		}
		return nil, uerr
	}

	logger.Scene("user").With("username", user.Username).With("ip", meta.IP).Info("用户登录成功")
	return &userdto.LoginResp{
		Token:     token,
		UserID:    user.ID,
		Username:  user.Username,
		Nickname:  displayName(user),
		Avatar:    deref(user.Avatar),
		Email:     user.Email,
		ExpiresAt: now.Add(ttl),
	}, nil
}

// Logout 登出：撤销当前令牌。
//
// Redis 与台账两步都做，而且 Redis 失败**不阻断**台账撤销：
// 只删 Redis 的话台账仍显示「在线」，用户会以为自己没退成功；
// 反过来只标记台账而留着 Redis 会话，表现就是「列表里没了，但那个浏览器还登录着」——
// 最糟的一种「退不掉」。宁可两边都失效。
func (s *Service) Logout(ctx context.Context, token string) (err error) {
	token = strings.TrimSpace(token)
	if token == "" || s.sm == nil {
		return nil
	}
	if uerr := cache.Delete(ctx, sessionKey(token)); uerr != nil {
		logger.Scene("user").Error(uerr, "登出时删除 Redis 会话失败")
		return errors.New(userenums.ErrLogoutFailed)
	}
	if _, rerr := s.sm.RevokeByTokenHash(ctx, tokenHash(token), time.Now()); rerr != nil {
		logger.Scene("user").Error(rerr, "登出时撤销会话台账失败")
	}
	return nil
}

// ResolveSession 校验令牌并返回会话；无效时返回 (nil, nil)。
//
// 未命中返回 nil 而不是 error：调用方（中间件）只关心「有没有登录」，
// 让它去分辨「会话过期」与「Redis 连不上」只会写错。
// 「判不出登录态就当未登录」是这个函数唯一的 fail-closed 选择。
func (s *Service) ResolveSession(ctx context.Context, token string) (sess *UserAuthSession, err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	got, gerr := cache.GetJSON[UserAuthSession](ctx, sessionKey(token))
	if gerr != nil {
		return nil, nil
	}
	if got.UserID == 0 {
		return nil, nil
	}
	return &got, nil
}

// ResolveVisitorID 把会话令牌解成 user id（实现 usercontract.VisitorIdentityResolver）。
//
// 不存在、过期、Redis 读不到一律返回 (0, false)：调用方要的是「认出来了没有」，
// 而不是「为什么没认出来」—— 后者会把「Redis 挂了」与「没登录」变成两条不同的路径，
// 而它们对访客的行为应当完全一样（去登录）。
func (s *Service) ResolveVisitorID(ctx context.Context, token string) (userID uint64, ok bool) {
	sess, err := s.ResolveSession(ctx, token)
	if err != nil || sess == nil || sess.UserID == 0 {
		return 0, false
	}
	return sess.UserID, true
}

// TouchSession 刷新设备最后活跃时间。
//
// 失败只记日志：用户已经登录成功这件事，不该因为「活跃时间没写上」而改变。
func (s *Service) TouchSession(ctx context.Context, sess *UserAuthSession) {
	if sess == nil || s.sm == nil || sess.RowID == 0 {
		return
	}
	if err := s.sm.TouchActive(ctx, sess.RowID, time.Now()); err != nil {
		logger.Scene("user").Error(err, "更新设备活跃时间失败")
	}
}

// ListSessions 列当前用户的登录设备，并标出哪一台是当前设备。
func (s *Service) ListSessions(ctx context.Context, userID uint64, currentToken string) (items []*userdto.SessionItem, err error) {
	if s.sm == nil {
		return nil, nil
	}
	rows, lerr := s.sm.ListActiveByUser(ctx, userID)
	if lerr != nil {
		return nil, lerr
	}
	cur := ""
	if strings.TrimSpace(currentToken) != "" {
		cur = tokenHash(currentToken)
	}
	items = make([]*userdto.SessionItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, &userdto.SessionItem{
			ID:             r.ID,
			UserAgent:      deref(r.UserAgent),
			IP:             deref(r.IP),
			Location:       deref(r.Location),
			LastActiveAt:   r.LastActiveAt,
			CreatedAt:      r.CreatedAt,
			LastActiveText: formatTime(r.LastActiveAt),
			CreatedText:    formatTime(r.CreatedAt),
			Current:        cur != "" && r.TokenHash == cur,
		})
	}
	return items, nil
}

// RevokeSession 踢掉某台设备。
//
// 归属校验在 model 的 SQL 条件里（`WHERE id = ? AND user_id = ?`），不在这里「先查后比」——
// 两段式在并发下有窗口，而且容易漏掉一处。
func (s *Service) RevokeSession(ctx context.Context, userID, rowID uint64) (err error) {
	if s.sm == nil {
		return errors.New(userenums.ErrSessionNotFound)
	}
	row, gerr := s.sm.GetByID(ctx, rowID)
	if gerr != nil {
		return gerr
	}
	if row == nil || row.UserID != userID || row.RevokedAt != nil {
		// 不存在、不属于当前用户、已经撤销过：一律按「没找到」处理。
		// 区分它们会让接口变成「某个 id 是不是别人的设备」的探测器。
		return errors.New(userenums.ErrSessionNotFound)
	}
	if _, uerr := s.sm.Revoke(ctx, userID, rowID, time.Now()); uerr != nil {
		return uerr
	}
	// 台账里存的就是 Redis key 的后半段，直接删得掉，不需要反查明文令牌。
	if err := s.dropRedisSession(row.TokenHash); err != nil {
		return err
	}
	return nil
}

// RevokeOtherSessions 退出其它所有设备（保留当前这一台）。
func (s *Service) RevokeOtherSessions(ctx context.Context, userID uint64, currentToken string) (revoked int64, err error) {
	if s.sm == nil {
		return 0, nil
	}
	keep := ""
	if strings.TrimSpace(currentToken) != "" {
		keep = tokenHash(currentToken)
	}
	// 先取列表再撤销：撤销之后就查不出「哪些行刚被撤掉」了，
	// 而 Redis 那边的会话必须按行逐个删。
	rows, lerr := s.sm.ListActiveByUser(ctx, userID)
	if lerr != nil {
		return 0, lerr
	}
	n, rerr := s.sm.RevokeAll(ctx, userID, keep, time.Now())
	if rerr != nil {
		return 0, rerr
	}
	for _, r := range rows {
		if keep != "" && r.TokenHash == keep {
			continue
		}
		if err := s.dropRedisSession(r.TokenHash); err != nil {
			return n, err
		}
	}
	return n, nil
}

// RevokeAllSessions 撤销该用户的全部会话（改密码、封禁时调用）。
func (s *Service) RevokeAllSessions(ctx context.Context, userID uint64) (err error) {
	if s.sm == nil {
		return nil
	}
	rows, lerr := s.sm.ListActiveByUser(ctx, userID)
	if lerr != nil {
		return lerr
	}
	if _, rerr := s.sm.RevokeAll(ctx, userID, "", time.Now()); rerr != nil {
		return rerr
	}
	for _, r := range rows {
		if err := s.dropRedisSession(r.TokenHash); err != nil {
			return err
		}
	}
	return nil
}

// dropRedisSession 删除令牌哈希对应的 Redis 会话。
// 踢设备场景下 Redis 删除失败必须返回错误（会话仍有效）。
func (s *Service) dropRedisSession(hash string) error {
	if strings.TrimSpace(hash) == "" {
		return nil
	}
	if err := cache.Delete(context.Background(), sessionKeyByHash(hash)); err != nil {
		logger.Scene("user").Error(err, "删除 Redis 会话失败")
		return errors.New(userenums.ErrLogoutFailed)
	}
	return nil
}

// optString 空串转 nil（库里用 NULL 表达「未提供」，不用空串）。
func optString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// deref 安全解引用（列可空，渲染时统一按空串处理）。
func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
