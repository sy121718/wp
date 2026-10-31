package feature

import (
	"context"
	"errors"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aiservice "go_wp/internal/module/ai/service"
)

func TestAIProviderBaseURLChangeDropsOldSecret(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "deepseek",
		DisplayName: "深度求索",
		APIKey:      testPlainKey,
	})
	if err != nil {
		t.Fatalf("前置：新建带密钥的供应商失败：%v", err)
	}
	if !p.HasAPIKey {
		t.Fatal("前置：新建后应有密钥")
	}

	t.Run("改地址不换密钥被拒绝", func(t *testing.T) {
		_, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
			ID:          p.ID,
			Version:     p.Version,
			ProviderKey: "deepseek",
			DisplayName: "深度求索",
			BaseURL:     "https://attacker-host.example.com/v1",
		})
		if !errors.Is(err, aiservice.ErrAPIKeyRequiredOnBaseURLChange) {
			t.Fatalf("改地址不换密钥应被拒绝，实际 err=%v", err)
		}
	})

	t.Run("被拒之后旧配置保持原样", func(t *testing.T) {
		cur, err := svc.GetProvider(ctx, p.ID)
		if err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		if cur.BaseURL == "https://attacker-host.example.com/v1" {
			t.Error("被拒的保存不应写进地址")
		}
		if !cur.HasAPIKey {
			t.Error("被拒的保存不应清掉原密钥")
		}
		if cur.Version != p.Version {
			t.Errorf("被拒的保存不应推进版本号：期望 %d，实际 %d", p.Version, cur.Version)
		}
	})

	t.Run("地址不变时留空表示不修改", func(t *testing.T) {
		cur, err := svc.GetProvider(ctx, p.ID)
		if err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		next, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
			ID:          cur.ID,
			Version:     cur.Version,
			ProviderKey: "deepseek",
			DisplayName: "深度求索二号",
			BaseURL:     cur.BaseURL,
		})
		if err != nil {
			t.Fatalf("地址未变、密钥留空应被接受，实际 err=%v", err)
		}
		if !next.HasAPIKey {
			t.Error("地址未变时留空不应清掉密钥")
		}
		if next.DisplayName != "深度求索二号" {
			t.Errorf("显示名称应更新，实际 %q", next.DisplayName)
		}
	})

	t.Run("带新密钥改地址被接受", func(t *testing.T) {
		cur, err := svc.GetProvider(ctx, p.ID)
		if err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		next, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
			ID:          cur.ID,
			Version:     cur.Version,
			ProviderKey: "deepseek",
			DisplayName: cur.DisplayName,
			BaseURL:     "https://gateway.example.com/v1",
			APIKey:      "sk-ai-test-replacement-0123456789ab",
		})
		if err != nil {
			t.Fatalf("带新密钥改地址应被接受，实际 err=%v", err)
		}
		if next.BaseURL != "https://gateway.example.com/v1" {
			t.Errorf("地址应更新，实际 %q", next.BaseURL)
		}
		if !next.HasAPIKey {
			t.Error("换密钥后应有密钥")
		}
	})
}
