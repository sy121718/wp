package config

import (
	"sync"

	"github.com/spf13/viper"

	"go_wp/pkg/lifecycle"
	"go_wp/pkg/logger"
)

type viperConfig = *viper.Viper

var (
	runtimeMu sync.Mutex
	runtime   = lifecycle.New(nil)
	suppress  = struct{ fn func() }{}
)

// InitComponents 初始化进程组件；失败逆序回滚且不重复关闭。
func InitComponents() error {
	cfg, err := GetViper()
	if err != nil {
		return err
	}
	if suppressWorkers.Load() {
		cfg.Set(keyRunWorker, false)
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	buildRuntimeLocked(cfg)
	logger.Scene("init").Info("开始初始化组件...")
	if err := runtime.Init(); err != nil {
		return err
	}
	logger.Scene("init").Info("组件初始化完成")
	return nil
}

// CloseComponents 逆序关闭已初始化组件。
func CloseComponents() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	logger.Scene("init").Info("开始关闭组件...")
	err := runtime.Close()
	if err != nil {
		return err
	}
	logger.Scene("init").Info("组件关闭完成")
	return nil
}

// ValidateReady 校验运行时已初始化且组件就绪。
func ValidateReady() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	return runtime.Ready()
}

// buildRuntimeLocked 把应用注册表翻译成生命周期组件。
func buildRuntimeLocked(cfg viperConfig) {
	components := make([]lifecycle.Component, 0, len(runtimeComponents))
	for _, c := range runtimeComponents {
		comp := lifecycle.Component{Name: c.Name, Critical: c.Critical}
		if c.Enabled != nil && cfg != nil {
			enabledFn := c.Enabled
			comp.Enabled = func() bool { return enabledFn(cfg) }
		}
		if c.Init != nil && cfg != nil {
			initFn := c.Init
			comp.Init = func() error { return initFn(cfg) }
		}
		comp.Ready = c.Ready
		comp.Close = c.Close
		components = append(components, comp)
	}
	runtime = lifecycle.New(components)
}
