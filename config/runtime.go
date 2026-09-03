package config

import (
	"errors"
	"fmt"
	"sync"

	"go_wp/pkg/logger"
)

var (
	runtimeMu           sync.Mutex
	initializedRegistry []runtimeComponent
	runtimeInited       bool
)

const (
	errComponentInitFailedPrefix = "component init failed"
	errComponentNotReadyPrefix   = "component not ready"
)

func InitComponents() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	if runtimeInited {
		return nil
	}

	cfg, err := GetViper()
	if err != nil {
		return err
	}

	for _, prepare := range runtimePreparers {
		prepare()
	}

	initialized := make([]runtimeComponent, 0, len(runtimeComponents))

	logger.Scene("init").Info("开始初始化组件...")
	for _, critical := range []bool{true, false} {
		for _, component := range runtimeComponents {
			if component.Critical != critical {
				continue
			}
			if component.Enabled != nil && !component.Enabled(cfg) {
				continue
			}

			if err := component.Init(cfg); err != nil {
				logger.Scene("init").With("component", component.Name).Error(err, "组件初始化失败")
				initErr := fmt.Errorf("%s [%s]: %w", errComponentInitFailedPrefix, component.Name, err)
				closeErr := closeComponents(initialized)
				if closeErr != nil {
					return errors.Join(initErr, closeErr)
				}
				return initErr
			}
			initialized = append(initialized, component)
		}
	}

	initializedRegistry = initialized
	runtimeInited = true
	logger.Scene("init").Info("组件初始化完成")
	return nil
}

func CloseComponents() error {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()

	if !runtimeInited {
		return nil
	}

	logger.Scene("init").Info("开始关闭组件...")
	closeErr := closeComponents(initializedRegistry)
	initializedRegistry = nil
	runtimeInited = false

	if closeErr != nil {
		return closeErr
	}

	logger.Scene("init").Info("组件关闭完成")
	return nil
}

func ValidateReady() error {
	runtimeMu.Lock()
	ready := runtimeInited
	components := append([]runtimeComponent(nil), initializedRegistry...)
	runtimeMu.Unlock()

	if !ready {
		return fmt.Errorf("%s [runtime]: runtime not initialized", errComponentNotReadyPrefix)
	}

	for _, component := range components {
		if component.Ready == nil {
			continue
		}
		if err := component.Ready(); err != nil {
			return fmt.Errorf("%s [%s]: %w", errComponentNotReadyPrefix, component.Name, err)
		}
	}
	return nil
}

func closeComponents(components []runtimeComponent) error {
	var closeErr error
	for i := len(components) - 1; i >= 0; i-- {
		component := components[i]
		if component.Close == nil {
			continue
		}
		if err := component.Close(); err != nil {
			logger.Scene("init").With("component", component.Name).Error(err, "组件关闭失败")
			closeErr = errors.Join(closeErr, fmt.Errorf("关闭组件 %s 失败: %w", component.Name, err))
		}
	}
	return closeErr
}
