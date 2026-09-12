package feature

// account_test.go — 发信账号的 feature 测试（issue #37）。
//
// 盯四件事，都是「出错会很难受」的地方：
//  1. 密码**只以密文落库**（库里的字符串不能包含明文）；
//  2. 响应**不回显密码**（只回答有没有设过）；
//  3. 更新时密码留空表示不改（否则编辑一次就把 SMTP 密码清掉）；
//  4. 未配置加密密钥时明确报错，而不是用弱密钥悄悄加密。

import (
	"context"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/pkg/crypto"
	"go_wp/pkg/mailer"
)

const testSecret = "unit-test-app-secret"

func newAccountFixture(t *testing.T) *fixture {
	f := newFixture(t)
	if f == nil {
		return nil
	}
	f.svc.SetCipherSecret(testSecret)
	return f
}

func TestAccountRequiresCipherSecret(t *testing.T) {
	f := newFixture(t) // 故意不设密钥
	if f == nil {
		return
	}
	_, err := f.svc.CreateAccount(context.Background(), &maildto.SaveAccountReq{
		Name: "系统通知", FromEmail: "a@b.com", Host: "smtp.example.com", Port: 587, Password: "secret",
	})
	if err == nil {
		t.Fatal("未配置加密密钥时必须报错（用弱密钥加密等于把密码明文写在库里）")
	}
}

func TestAccountPasswordStoredEncryptedNotEchoed(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	const plain = "74rGAtDoaJzzA09Q"

	item, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "系统通知", Purpose: mailmodel.AccountPurposeTransactional,
		FromName: "客服", FromEmail: "admin@clker.cn", ReplyTo: "kf@clker.cn",
		Provider: "smtp", Host: "mail.clker.cn", Port: 587, Username: "admin@clker.cn",
		Password: plain, Encryption: mailer.EncryptionStartTLS, RatePerHour: 200,
	})
	if err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}
	if !item.HasPassword {
		t.Fatal("HasPassword 应为 true")
	}
	if !item.IsDefault {
		t.Fatal("第一个 transactional 账号应自动成为默认")
	}

	// 库里存的必须是密文，且不能包含明文片段。
	row, err := f.m.GetAccount(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.PasswordCipher == nil || *row.PasswordCipher == "" {
		t.Fatal("密码列不该为空")
	}
	if *row.PasswordCipher == plain {
		t.Fatal("密码被明文落库了")
	}
	if strings.Contains(*row.PasswordCipher, plain) {
		t.Fatal("密文里出现了明文")
	}

	// 能正确解密回来（发信时要还原）。
	back, err := crypto.Decrypt(*row.PasswordCipher, testSecret)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if back != plain {
		t.Fatalf("解密结果不对: %q", back)
	}

	// 列表响应不含任何密码字段。
	list, err := f.svc.ListAccounts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应有 1 个账号，实际 %d", len(list))
	}
	if list[0].HasPassword != true || list[0].FromEmail != "admin@clker.cn" {
		t.Fatalf("列表条目字段不对: %+v", list[0])
	}
}

func TestAccountUpdateKeepsPasswordWhenBlank(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	created, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "系统通知", FromEmail: "a@b.com", Host: "smtp.example.com", Port: 587, Password: "original",
	})
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := f.m.GetAccount(ctx, created.ID)
	origCipher := *orig.PasswordCipher

	// 只改名字，密码留空
	if _, err = f.svc.UpdateAccount(ctx, &maildto.SaveAccountReq{
		ID: created.ID, Name: "改名后的账号", FromEmail: "a@b.com", Host: "smtp.example.com", Port: 587,
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	after, _ := f.m.GetAccount(ctx, created.ID)
	if after.Name != "改名后的账号" {
		t.Fatal("名字没更新上")
	}
	if after.PasswordCipher == nil || *after.PasswordCipher != origCipher {
		t.Fatal("密码留空时不该改动密码（否则编辑一次就把 SMTP 密码清掉）")
	}
}

func TestSetDefaultAccountIsExclusivePerPurpose(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	base := func(name, purpose string) *maildto.SaveAccountReq {
		return &maildto.SaveAccountReq{Name: name, Purpose: purpose, FromEmail: "a@b.com", Host: "smtp.example.com", Port: 587, Password: "p"}
	}
	a1, err := f.svc.CreateAccount(ctx, base("事务一", mailmodel.AccountPurposeTransactional))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := f.svc.CreateAccount(ctx, base("事务二", mailmodel.AccountPurposeTransactional))
	if err != nil {
		t.Fatal(err)
	}
	a3, err := f.svc.CreateAccount(ctx, base("营销一", mailmodel.AccountPurposeMarketing))
	if err != nil {
		t.Fatal(err)
	}
	if !a1.IsDefault {
		t.Fatal("第一个事务账号应为默认")
	}
	if a2.IsDefault {
		t.Fatal("第二个事务账号不该自动默认")
	}
	if !a3.IsDefault {
		t.Fatal("营销用途的第一个账号也应为该用途的默认")
	}

	if err = f.svc.SetDefaultAccount(ctx, a2.ID); err != nil {
		t.Fatalf("切换默认失败: %v", err)
	}
	// 同用途只有一个默认；另一个用途的默认不受影响。
	svcDefault, err := f.svc.ListAccounts(ctx, mailmodel.AccountPurposeTransactional)
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, item := range svcDefault {
		if item.IsDefault {
			defaults++
			if item.ID != a2.ID {
				t.Fatalf("默认应已切到 a2，实际 %d", item.ID)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("同用途只能有一个默认，实际 %d", defaults)
	}
}

var _ = mailservice.Service{}
