package userservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"go_wp/internal/module/user/dto"
	"go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/cache"
	"go_wp/pkg/crypto"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	sessionTokenBytes = 32

	sessionRedisPrefix = "gwp:userauth:sess:"

	userSessionTTL         = 24 * time.Hour
	userSessionRememberTTL = 7 * 24 * time.Hour

	loginFailureLockThreshold = 5
	loginFailureLockDuration  = 30 * time.Minute
)

type UserAuthSession struct {
	Token    string `json:"-"`
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`

	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Location  string `json:"location"`

	IssuedAt     int64 `json:"issued_at"`
	LastActiveAt int64 `json:"last_active_at"`
}

type SessionMeta struct {
	IP        string
	UserAgent string
	Location  string
}

func SessionTTLFor(rememberMe bool) time.Duration {
	if rememberMe {
		return userSessionRememberTTL
	}
	return userSessionTTL
}

func newSessionToken() (token string, err error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func tokenHash(token string) string { return crypto.Sha256(token) }

func sessionKey(token string) string {
	return sessionRedisPrefix + tokenHash(strings.TrimSpace(token))
}

func sessionKeyByHash(hash string) string { return sessionRedisPrefix + hash }

func (s *Service) Login(ctx context.Context, req *userdto.LoginReq, meta SessionMeta) (res *userdto.LoginResp, err error) {
	if s.m == nil {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	if req == nil {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	account := strings.TrimSpace(req.Account)
	password := req.Password
	if account == "" || password == "" {
		return nil, userenums.Err(userenums.ErrLoginRequired)
	}

	user, ferr := s.m.GetByUsername(ctx, account)
	if ferr != nil {
		user, ferr = s.m.GetByEmail(ctx, account)
	}
	if ferr != nil {

		return nil, userenums.Err(userenums.ErrBadCredentials)
	}

	if user.LockedUntilTime != nil && time.Now().Before(*user.LockedUntilTime) {
		remain := time.Until(*user.LockedUntilTime).Round(time.Minute)
		if remain < time.Minute {
			remain = time.Minute
		}

		return nil, errors.New(userenums.ErrAccountLocked + "|" + remain.String())
	}

	if user.Status == usermodel.UserStatusPending {
		return nil, userenums.Err(userenums.ErrAccountPending)
	}
	if user.Status != usermodel.UserStatusActive {
		return nil, userenums.Err(userenums.ErrAccountDisabled)
	}

	if strings.TrimSpace(user.Password) == "" {
		return nil, userenums.Err(userenums.ErrPasswordLoginUnavailable)
	}

	if cerr := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); cerr != nil {
		if uerr := s.m.IncrLoginFailure(ctx, user.ID, loginFailureLockThreshold, loginFailureLockDuration); uerr != nil {

			logger.Scene("user").Error(uerr, "记录登录失败状态失败")
		}
		return nil, userenums.Err(userenums.ErrBadCredentials)
	}

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
		ExpiresAt: utils.NewJSONTime(now.Add(ttl)),
	}, nil
}

func (s *Service) Logout(ctx context.Context, token string) (err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	hash := tokenHash(token)

	var userID uint64
	if sess, gerr := cache.GetJSON[UserAuthSession](ctx, sessionKeyByHash(hash)); gerr == nil {
		userID = sess.UserID
	}
	if uerr := cache.Delete(ctx, sessionKeyByHash(hash)); uerr != nil {
		logger.Scene("user").Error(uerr, "登出时删除 Redis 会话失败")
		return userenums.Err(userenums.ErrLogoutFailed)
	}
	if userID != 0 {
		if rerr := unindexSession(ctx, userID, hash); rerr != nil {
			logger.Scene("user").Error(rerr, "登出时清理设备索引失败")
		}
	}
	return nil
}

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

	got.Token = token
	return &got, nil
}

func (s *Service) ResolveVisitorID(ctx context.Context, token string) (userID uint64, ok bool) {
	sess, err := s.ResolveSession(ctx, token)
	if err != nil || sess == nil || sess.UserID == 0 {
		return 0, false
	}
	return sess.UserID, true
}

func (s *Service) TouchSession(ctx context.Context, sess *UserAuthSession) {
	if sess == nil || sess.UserID == 0 || strings.TrimSpace(sess.Token) == "" {
		return
	}
	sess.LastActiveAt = time.Now().Unix()

	if _, err := cache.SetJSONKeepTTLIfExists(ctx, sessionKey(sess.Token), sess); err != nil {
		logger.Scene("user").Error(err, "更新设备活跃时间失败")
	}
}

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
			LastActiveAt:   utils.NewJSONTimePtr(&active),
			CreatedAt:      utils.NewJSONTimePtr(&issued),
			LastActiveText: formatTime(&active),
			CreatedText:    formatTime(&issued),
			Current:        cur != "" && hash == cur,
		})
	}
	return items, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID uint64, sessionHash string) (err error) {
	sessionHash = strings.TrimSpace(sessionHash)
	if sessionHash == "" {
		return userenums.Err(userenums.ErrSessionNotFound)
	}
	sess, gerr := cache.GetJSON[UserAuthSession](ctx, sessionKeyByHash(sessionHash))
	if gerr != nil || sess.UserID == 0 || sess.UserID != userID {
		return userenums.Err(userenums.ErrSessionNotFound)
	}
	return dropSession(ctx, userID, sessionHash)
}

func (s *Service) RevokeOtherSessions(ctx context.Context, userID uint64, currentToken string) (revoked int64, err error) {
	keep := ""
	if strings.TrimSpace(currentToken) != "" {
		keep = tokenHash(currentToken)
	}
	hashes, lerr := listSessionHashes(ctx, userID)
	if lerr != nil {
		return 0, lerr
	}

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

func optString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

const userSessionIndexPrefix = "gwp:userauth:user:"

func userSessionIndexKey(userID uint64) string {
	return userSessionIndexPrefix + strconv.FormatUint(userID, 10) + ":sess"
}

func indexSession(ctx context.Context, userID uint64, hash string, issuedAt time.Time) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	key := userSessionIndexKey(userID)
	if err = client.ZAdd(ctx, key, redis.Z{
		Score:  float64(issuedAt.Unix()),
		Member: hash,
	}).Err(); err != nil {
		return err
	}
	return client.Expire(ctx, key, userSessionRememberTTL).Err()
}

func listSessionHashes(ctx context.Context, userID uint64) (hashes []string, err error) {
	client, err := cache.GetRedis()
	if err != nil {
		return nil, err
	}
	return client.ZRevRange(ctx, userSessionIndexKey(userID), 0, -1).Result()
}

func unindexSession(ctx context.Context, userID uint64, hash string) error {
	client, err := cache.GetRedis()
	if err != nil {
		return err
	}
	return client.ZRem(ctx, userSessionIndexKey(userID), hash).Err()
}

func dropSession(ctx context.Context, userID uint64, hash string) error {
	if strings.TrimSpace(hash) == "" {
		return nil
	}
	if err := cache.Delete(ctx, sessionKeyByHash(hash)); err != nil {
		return err
	}
	return unindexSession(ctx, userID, hash)
}

func SetSessionForTest(ctx context.Context, sess *UserAuthSession, ttl time.Duration) error {
	return cache.SetJSON(ctx, sessionKey(sess.Token), sess, ttl)
}

func SessionHashForTest(token string) string { return tokenHash(strings.TrimSpace(token)) }

func SessionTTLForTest(ctx context.Context, token string) (time.Duration, error) {
	return cache.TTL(ctx, sessionKey(token))
}
