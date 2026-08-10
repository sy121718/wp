package logger

import "testing"

func TestEnsureInitedReturnsErrorWhenLoggerNotInitialized(t *testing.T) {
	mu.Lock()
	oldInited := inited
	oldCfg := cfg
	inited = false
	cfg = runtimeConfig{}
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		inited = oldInited
		cfg = oldCfg
		mu.Unlock()
		_ = clearSceneLoggers()
	})

	if err := ensureInited(); err == nil {
		t.Fatalf("未初始化 logger 时应返回错误")
	}
}
