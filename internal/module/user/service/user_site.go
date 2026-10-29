package userservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
)

func (s *Service) SiteSession(userID uint64) userdto.SiteSessionResp {
	return userdto.SiteSessionResp{Authenticated: userID != 0}
}

// SiteDevices 复用设备台账，只把会话存储哈希替换成当前会话专用的设备句柄。
func (s *Service) SiteDevices(ctx context.Context, userID uint64, token string) ([]userdto.SiteDevice, error) {
	items, err := s.SessionsOf(ctx, userID, token)
	if err != nil {
		return nil, err
	}
	out := make([]userdto.SiteDevice, 0, len(items))
	for _, item := range items {
		out = append(out, userdto.SiteDevice{
			ID: siteDeviceID(token, item.ID), UserAgent: item.UserAgent, IP: item.IP,
			Location: item.Location, LastActiveAt: item.LastActiveAt, CreatedAt: item.CreatedAt,
			Current: item.Current,
		})
	}
	return out, nil
}

// RevokeSiteDevice 仅在本人的设备集合中解析句柄，不接受客户端提供的存储哈希。
func (s *Service) RevokeSiteDevice(ctx context.Context, userID uint64, token, id string) error {
	items, err := s.SessionsOf(ctx, userID, token)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !item.Current && hmac.Equal([]byte(siteDeviceID(token, item.ID)), []byte(strings.TrimSpace(id))) {
			return s.RevokeSession(ctx, userID, item.ID)
		}
	}
	return userenums.Err(userenums.ErrSessionNotFound)
}

func siteDeviceID(token, hash string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(hash))
	return hex.EncodeToString(mac.Sum(nil))
}
