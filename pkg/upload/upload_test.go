package upload

import (
	"testing"

	"github.com/spf13/viper"
)

func TestInitRegistersBuiltinProvidersWhenRegistryIsEmpty(t *testing.T) {
	oldProviders := providers
	oldInited := inited
	oldConfigSource := configSource
	oldDefaultProvider := defaultProvider
	oldRules := uploadRules

	t.Cleanup(func() {
		providers = oldProviders
		inited = oldInited
		configSource = oldConfigSource
		defaultProvider = oldDefaultProvider
		uploadRules = oldRules
	})

	providers = map[string]*providerEntry{}
	inited = false
	configSource = nil
	defaultProvider = "local"

	cfg := viper.New()
	cfg.Set("upload.enabled", true)
	cfg.Set("upload.default_provider", "local")

	if err := Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}

	if _, ok := providers["local"]; !ok {
		t.Fatalf("应自动注册 local provider")
	}
	if _, ok := providers["qiniu"]; !ok {
		t.Fatalf("应自动注册 qiniu provider")
	}
}
