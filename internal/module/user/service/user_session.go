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
// # 两层各放什么
//
//   - cookie（签名）：只放令牌本身，只是「你是谁」的凭据，不含业务字段；
//   - Redis：会话状态 + 设备台账 —— 既决定「这个请求算不算已登录」，也提供「我登录了哪些设备」。
//
// 访客会话**不落库**：Redis 是唯一真源。设备视图靠用户维度的索引 ZSET 支撑，见 user_session_store.go。
//
// # Redis key 用令牌哈希而不是明文令牌
//
// key 是 `gwp:userauth:sess:<sha256(令牌)>`，索引成员也是同一个哈希。这样撤销某台设备时不需要
// 「由记录反查令牌」——那种反查要么存明文（等于白做哈希），要么额外维护一张映射表（多一处会
// 不一致的状态）。会话 JSON 里也不带明文令牌：Token 字段只存在于进程内。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
// 设备信息（IP / UA / 来源地）也在这里：设备列表的数据源就是它，没有第二处台账。
type UserAuthSession struct {
	// Token 只在进程内携带，**不序列化**：cookie 里的凭据不该出现在任何可读存储里。
	// 唯一消费方是 TouchSession —— 它要定位自己那条会话 key。
	Token    string `json:"-"`
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
	// 设备信息由登录时的请求采集（客户端不可伪造）。
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Location  string `json:"location"`
	// IssuedAt 会话建立时间（秒）；LastActiveAt 最近一次活跃时间（秒）。
	IssuedAt     int64 `json:"issued_at"`
	LastActiveAt int64 `json:"last_active_at"`
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
	if s.m == nil {
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
		// enums 常量改为 i18n key 后（审计 I18N-002），带参数的消息不能再靠 fmt.Errorf
		// 注入 —— key 本身没有 %s（vet 会直接报「arguments but no formatting directives」）。
		// 改用响应层的 key|param 协议：出站时按语言取到「账号已锁定，请在 %s 后重试」再注入。
		return nil, errors.New(userenums.ErrAccountLocked + "|" + remain.String())
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

	// 先写设备索引，再写会话本体：索引先有归属，会话写失败时摘掉即可。
	// 反过来会出现「会话在 Redis、索引里没有」——用户显示在线，设备列表里却找不到它，
	// 于是永远踢不掉（列表的自愈只能清过期成员，清不了这种缺成员）。
	hash := tokenHash(token)
	if uerr := indexSession(ctx, user.ID, hash, now); uerr != nil {
		return nil, uerr
	}

	sess := &UserAuthSession{
		Token:        token,
		UserID:       user.ID,
		Username:     user.Username,
		Nickname:     displayName(user),
		Avatar:       deref(user.Avatar),
		Email:        user.Email,
		IP:           strings.TrimSpace(meta.IP),
		UserAgent:    strings.TrimSpace(meta.UserAgent),
		Location:     strings.TrimSpace(meta.Location),
		IssuedAt:     now.Unix(),
		LastActiveAt: now.Unix(),
	}
	if uerr := cache.SetJSON(ctx, sessionKeyByHash(hash), sess, ttl); uerr != nil {
		// 会话写不进去 → 这次登录并不成立：把刚写的索引成员摘掉，
		// 不留一条「索引里有、会话不存在」的幽灵设备。
		if rerr := unindexSession(ctx, user.ID, hash); rerr != nil {
			logger.Scene("user").Error(rerr, "登录失败回滚设备索引失败")
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
// 会话本体与设备索引都要清：只删会话会让索引里留一条指向空会话的成员（列表会自愈，
// 但用户会先看到一台假设备）；反过来只摘索引而留着 Redis 会话，表现就是「列表里没了，
// 但那个浏览器还登录着」—— 最糟的一种「退不掉」。宁可两边都失效。
func (s *Service) Logout(ctx context.Context, token string) (err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	hash := tokenHash(token)
	// 索引 key 是按用户分的，所以先读一次拿 userID。会话已过期时读不到 ——
	// 那种情况下索引成员交给列表自愈，不当作失败。
	var userID uint64
	if sess, gerr := cache.GetJSON[UserAuthSession](ctx, sessionKeyByHash(hash)); gerr == nil {
		userID = sess.UserID
	}
	if uerr := cache.Delete(ctx, sessionKeyByHash(hash)); uerr != nil {
		logger.Scene("user").Error(uerr, "登出时删除 Redis 会话失败")
		return errors.New(userenums.ErrLogoutFailed)
	}
	if userID != 0 {
		if rerr := unindexSession(ctx, userID, hash); rerr != nil {
			logger.Scene("user").Error(rerr, "登出时清理设备索引失败")
		}
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
	// Token 不进存储（json:"-"），在这里回填成进程内字段：
	// TouchSession 要靠它定位自己那条 key。
	got.Token = token
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
	if sess == nil || sess.UserID == 0 || strings.TrimSpace(sess.Token) == "" {
		return
	}
	sess.LastActiveAt = time.Now().Unix()
	// 用 KEEPTTL 覆盖写：活跃时间要更新，但**不能**顺手把会话续期 ——
	// 那会让「24 小时的会话」变成「只要一直在点就一直有效」。
	if err := cache.SetJSONKeepTTL(ctx, sessionKey(sess.Token), sess); err != nil {
		logger.Scene("user").Error(err, "更新设备活跃时间失败")
	}
}

// ListSessions 列当前用户的登录设备，并标出哪一台是当前设备。
//
// 数据源是 Redis：索引 ZSET 给出设备哈希，逐个读会话 JSON 拿设备信息。
// 索引里指向已过期会话的成员就地摘掉 —— TTL 只挂在会话 key 上，索引不会自己变干净。
func (s *Service) ListSessions(ctx context.Context, userID uint64, currentToken string) (items []*userdto.SessionItem, err error) {
	hashes, lerr := listSessionHashes(ctx, userID)
	if lerr != nil {
		return nil, lerr
	}
	cur := ""
	if strings.TrimSpace(currentToken) != "" {
		cur = tokenHash(currentToken)
	}
	items = make([]*userdto.SessionItem, 0, len(hashes))
	for _, hash := range hashes {
		sess, gerr := cache.GetJSON[UserAuthSession](ctx, sessionKeyByHash(hash))
		if gerr != nil || sess.UserID == 0 {
			if uerr := unindexSession(ctx, userID, hash); uerr != nil {
				logger.Scene("user").Error(uerr, "清理过期设备索引失败")
			}
			continue
		}
		issued := time.Unix(sess.IssuedAt, 0)
		active := time.Unix(sess.LastActiveAt, 0)
		items = append(items, &userdto.SessionItem{
			ID:             hash,
			UserAgent:      strings.TrimSpace(sess.UserAgent),
			IP:             strings.TrimSpace(sess.IP),
			Location:       strings.TrimSpace(sess.Location),
			LastActiveAt:   &active,
			CreatedAt:      &issued,
			LastActiveText: formatTime(&active),
			CreatedText:    formatTime(&issued),
			Current:        cur != "" && hash == cur,
		})
	}
	return items, nil
}

// RevokeSession 踢掉某台设备。
//
// 归属校验放在读出会话之后：会话 JSON 里的 user_id 必须等于调用者。
// 「不存在」与「不是你的设备」返回同一个错误 —— 区分它们，接口就变成了探测器。
func (s *Service) RevokeSession(ctx context.Context, userID uint64, sessionHash string) (err error) {
	sessionHash = strings.TrimSpace(sessionHash)
	if sessionHash == "" {
		return errors.New(userenums.ErrSessionNotFound)
	}
	sess, gerr := cache.GetJSON[UserAuthSession](ctx, sessionKeyByHash(sessionHash))
	if gerr != nil || sess.UserID == 0 || sess.UserID != userID {
		return errors.New(userenums.ErrSessionNotFound)
	}
	return dropSession(ctx, userID, sessionHash)
}

// RevokeOtherSessions 退出其它所有设备（保留当前这一台）。
func (s *Service) RevokeOtherSessions(ctx context.Context, userID uint64, currentToken string) (revoked int64, err error) {
	keep := ""
	if strings.TrimSpace(currentToken) != "" {
		keep = tokenHash(currentToken)
	}
	hashes, lerr := listSessionHashes(ctx, userID)
	if lerr != nil {
		return 0, lerr
	}
	// 索引成员就是撤销对象；过期成员在这里顺带被摘掉（dropSession 会容忍空会话）。
	for _, hash := range hashes {
		if keep != "" && hash == keep {
			continue
		}
		if derr := dropSession(ctx, userID, hash); derr != nil {
			return revoked, derr
		}
		revoked++
	}
	return revoked, nil
}

// RevokeAllSessions 撤销该用户的全部会话（改密码、封禁时调用）。
func (s *Service) RevokeAllSessions(ctx context.Context, userID uint64) (err error) {
	hashes, lerr := listSessionHashes(ctx, userID)
	if lerr != nil {
		return lerr
	}
	for _, hash := range hashes {
		if derr := dropSession(ctx, userID, hash); derr != nil {
			return derr
		}
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
