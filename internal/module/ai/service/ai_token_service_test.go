package aiservice_test

import (
	"context"
	"strings"
	"testing"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/crypto"
)

func tokenTestScopeOK(p string) bool {
	return p == "order:list" || p == "ai:chat"
}

func newTokenService(t *testing.T) (*aiservice.AccessTokenService, *aimodel.AccessTokenModel) {
	t.Helper()
	_, db := newChatTestService(t)
	m := aimodel.NewAccessTokenModel(db)
	svc := aiservice.NewAccessTokenService(m)
	svc.SetScopeValidator(tokenTestScopeOK)
	return svc, m
}

func createToken(t *testing.T, svc *aiservice.AccessTokenService, name string, scopes []string, expiresAt string) *aidto.TokenCreateResp {
	t.Helper()
	res, err := svc.Create(context.Background(), &aidto.TokenCreateReq{
		Name:      name,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
		UserID:    testUserID,
	})
	if err != nil {
		t.Fatalf("签发令牌失败：%v", err)
	}
	return res
}

func TestTokenCreateKeepsOnlyHashInDB(t *testing.T) {
	svc, m := newTokenService(t)
	res := createToken(t, svc, "外部看板", []string{"order:list"}, "")

	plain := res.Token
	if !strings.HasPrefix(plain, "wp_") {
		t.Errorf("明文应有 wp_ 前缀，实际 %q", plain)
	}
	if len([]rune(plain)) < 40 {
		t.Errorf("明文太短（随机部分不足），实际 %d 字符", len([]rune(plain)))
	}
	if res.Item.TokenPrefix == plain {
		t.Error("列表项不该回明文：TokenPrefix 必须是截断后的展示片段")
	}
	if !strings.HasPrefix(plain, res.Item.TokenPrefix) {
		t.Errorf("展示前缀应是明文的开头，实际 %q vs %q", res.Item.TokenPrefix, plain)
	}

	row, err := m.FindByHash(context.Background(), crypto.Sha256(plain))
	if err != nil || row == nil {
		t.Fatalf("按哈希应能查到这一行：row=%v err=%v", row, err)
	}
	if row.TokenHash == plain || strings.Contains(row.TokenHash, plain) {
		t.Error("库里不该出现明文")
	}
	if row.Status != int16(aienums.TokenStatusActive) {
		t.Errorf("新建令牌应为启用，实际 %d", row.Status)
	}
}

func TestTokenCreateValidatesInput(t *testing.T) {
	svc, _ := newTokenService(t)
	cases := []struct {
		name    string
		req     aidto.TokenCreateReq
		wantKey string
	}{
		{"用途为空", aidto.TokenCreateReq{Name: "  ", Scopes: []string{"order:list"}, UserID: testUserID}, aienums.ErrTokenNameRequired},
		{"scope 为空", aidto.TokenCreateReq{Name: "x", Scopes: nil, UserID: testUserID}, aienums.ErrTokenScopeRequired},
		{"scope 全是空白", aidto.TokenCreateReq{Name: "x", Scopes: []string{" ", ""}, UserID: testUserID}, aienums.ErrTokenScopeRequired},
		{"含未知权限点", aidto.TokenCreateReq{Name: "x", Scopes: []string{"order:lst"}, UserID: testUserID}, aienums.ErrTokenScopeUnknown},
		{"没带账号", aidto.TokenCreateReq{Name: "x", Scopes: []string{"order:list"}}, aienums.ErrUserRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := c.req
			_, err := svc.Create(context.Background(), &req)
			if err == nil {
				t.Fatal("应拒绝，实际通过")
			}
			if err.Error() != c.wantKey {
				t.Errorf("错误应为 %q，实际 %q", c.wantKey, err.Error())
			}
		})
	}
}

func TestTokenCreateScopesDedupKeepsOrder(t *testing.T) {
	svc, _ := newTokenService(t)
	res := createToken(t, svc, "去重", []string{"ai:chat", "order:list", "ai:chat", " ai:chat "}, "")
	want := []string{"ai:chat", "order:list"}
	if len(res.Item.Scopes) != len(want) {
		t.Fatalf("scope 应去重为 %v，实际 %v", want, res.Item.Scopes)
	}
	for i := range want {
		if res.Item.Scopes[i] != want[i] {
			t.Errorf("scope[%d] 应为 %q，实际 %q", i, want[i], res.Item.Scopes[i])
		}
	}
}

func TestTokenCreateUnsetValidatorFailsClosed(t *testing.T) {
	_, db := newChatTestService(t)
	svc := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))

	_, err := svc.Create(context.Background(), &aidto.TokenCreateReq{
		Name: "x", Scopes: []string{"order:list"}, UserID: testUserID,
	})
	if err == nil || err.Error() != aienums.ErrTokenScopeUnknown {
		t.Fatalf("未注入校验端口时应按未知权限点拒，实际 %v", err)
	}
}

func TestTokenExpiryIsEndOfDay(t *testing.T) {
	svc, _ := newTokenService(t)
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	res := createToken(t, svc, "有期限", []string{"order:list"}, tomorrow)
	if res.Item.ExpiresAt == nil {
		t.Fatal("给了过期日就该有到期时刻")
	}
	want := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, 2)
	got := time.Time(*res.Item.ExpiresAt).UTC()
	if !got.Equal(want) {
		t.Errorf("过期时刻应为 %v（该日结束），实际 %v", want, got)
	}

	_, err := svc.Create(context.Background(), &aidto.TokenCreateReq{
		Name: "过期日已过", Scopes: []string{"order:list"}, ExpiresAt: "2020-01-01", UserID: testUserID,
	})
	if err == nil || err.Error() != aienums.ErrTokenExpiredInPast {
		t.Fatalf("过期日已过去应被拒，实际 %v", err)
	}

	_, err = svc.Create(context.Background(), &aidto.TokenCreateReq{
		Name: "格式错", Scopes: []string{"order:list"}, ExpiresAt: "2026/01/01", UserID: testUserID,
	})
	if err == nil || err.Error() != aienums.ErrInvalidParam {
		t.Fatalf("非法日期应报参数错误，实际 %v", err)
	}
}

func TestTokenVerify(t *testing.T) {
	svc, m := newTokenService(t)
	created := createToken(t, svc, "校验用", []string{"order:list", "ai:chat"}, "")

	id, err := svc.Verify(context.Background(), created.Token)
	if err != nil {
		t.Fatalf("校验应通过：%v", err)
	}
	if id.UserID != testUserID {
		t.Errorf("账号应为 %d，实际 %d", testUserID, id.UserID)
	}
	if !id.HasScope("order:list") || !id.HasScope("ai:chat") {
		t.Errorf("scope 应含两条声明，实际 %v", id.Scopes)
	}
	if id.HasScope("product:list") {
		t.Error("未声明的权限点不该通过")
	}
	if id.HasScope("") {
		t.Error("空权限点不该通过")
	}

	rows, err := m.List(context.Background(), testUserID, 10)
	if err != nil || len(rows) == 0 {
		t.Fatalf("列表应有记录：%v", err)
	}
	if rows[0].LastUsedTime == nil {
		t.Error("校验成功后应记 last_used_time")
	}
}

func TestTokenVerifyFailsClosed(t *testing.T) {
	svc, _ := newTokenService(t)
	bad := []struct {
		name  string
		plain string
	}{
		{"空串", ""},
		{"只有空白", "   "},
		{"前缀不对", "sk_abcdefg"},
		{"看起来对但不存在", "wp_0000000000000000000000000000000000000000"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.Verify(context.Background(), c.plain); err == nil ||
				err.Error() != aienums.ErrTokenInvalid {
				t.Fatalf("应为 %q，实际 %v", aienums.ErrTokenInvalid, err)
			}
		})
	}

	created := createToken(t, svc, "待撤销", []string{"order:list"}, "")
	if err := svc.Revoke(context.Background(), created.Item.ID); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if _, err := svc.Verify(context.Background(), created.Token); err == nil ||
		err.Error() != aienums.ErrTokenInvalid {
		t.Fatalf("撤销后应为 %q，实际 %v", aienums.ErrTokenInvalid, err)
	}
}

func TestTokenRevokeIsIdempotentByStatus(t *testing.T) {
	svc, m := newTokenService(t)
	created := createToken(t, svc, "撤销两次", []string{"order:list"}, "")

	if err := svc.Revoke(context.Background(), created.Item.ID); err != nil {
		t.Fatalf("首次撤销失败：%v", err)
	}
	err := svc.Revoke(context.Background(), created.Item.ID)
	if err == nil || err.Error() != aienums.ErrTokenNotFound {
		t.Fatalf("重复撤销应为 %q（它已经不是启用的了），实际 %v", aienums.ErrTokenNotFound, err)
	}

	rows, err := m.List(context.Background(), testUserID, 50)
	if err != nil {
		t.Fatalf("列表失败：%v", err)
	}
	var found *aimodel.AIAccessTokenEntity
	for i := range rows {
		if rows[i].ID == created.Item.ID {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatal("撤销不该删行")
	}
	if found.Status != int16(aienums.TokenStatusRevoked) || found.RevokedTime == nil {
		t.Errorf("状态应为已撤销且有撤销时刻，实际 status=%d revoked=%v", found.Status, found.RevokedTime)
	}
}

func TestTokenListScope(t *testing.T) {
	svc, m := newTokenService(t)
	created := createToken(t, svc, "自己的", []string{"order:list"}, "")

	other := &aimodel.AIAccessTokenEntity{
		Name: "别人的", UserID: testUserID + 1000, TokenPrefix: "wp_other0000",
		TokenHash: "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f",
		Scopes:    aimodel.StringList{"order:list"}, Status: int16(aienums.TokenStatusActive),
	}
	if err := m.Insert(context.Background(), other); err != nil {
		t.Fatalf("造他人令牌失败：%v", err)
	}

	mine, err := svc.List(context.Background(), &aidto.TokenListReq{UserID: testUserID, Limit: 50})
	if err != nil {
		t.Fatalf("列表失败：%v", err)
	}
	for _, item := range mine {
		if item.UserID != testUserID {
			t.Errorf("不带 all 时不该出现别人的令牌：%+v", item)
		}
	}

	all, err := svc.List(context.Background(), &aidto.TokenListReq{UserID: testUserID, All: true, Limit: 50})
	if err != nil {
		t.Fatalf("全站列表失败：%v", err)
	}
	var sawOther, sawMine bool
	for _, item := range all {
		if item.ID == other.ID {
			sawOther = true
		}
		if item.ID == created.Item.ID {
			sawMine = true
		}
	}
	if !sawOther || !sawMine {
		t.Errorf("all=true 应同时看到自己与别人的令牌（mine=%v other=%v）", sawMine, sawOther)
	}

	if _, err := svc.List(context.Background(), &aidto.TokenListReq{Limit: 10}); err == nil ||
		err.Error() != aienums.ErrUserRequired {
		t.Fatalf("缺账号且不带 all 应报 %q，实际 %v", aienums.ErrUserRequired, err)
	}
}
