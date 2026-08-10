package i18n

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

// DBProvider 由外部注入的数据库获取函数。
// 调用方在初始化阶段通过 SetDBProvider 注入，解耦 i18n 包对具体 database 包的依赖。
var (
	dbProvider     func() (*gorm.DB, error)
	dbProviderMu   sync.Mutex
	refreshMu      sync.Mutex
	refreshCancel  context.CancelFunc
	refreshWG      sync.WaitGroup
)

// SetDBProvider 注入数据库获取函数。
func SetDBProvider(fn func() (*gorm.DB, error)) {
	dbProviderMu.Lock()
	defer dbProviderMu.Unlock()
	dbProvider = fn
}

// LoadCache 从数据库加载多语言数据到内存
func LoadCache() error {
	dbProviderMu.Lock()
	provider := dbProvider
	dbProviderMu.Unlock()

	if provider == nil {
		return fmt.Errorf("i18n: DBProvider 未注入，请先调用 SetDBProvider()")
	}

	db, err := provider()
	if err != nil {
		return fmt.Errorf("获取数据库实例失败: %w", err)
	}

	var rows []struct {
		ItemKey  string `gorm:"column:item_key"`
		Lang     string
		Value    string
		HttpCode *int
	}

	err = db.Table("sys_i18n").
		Select("item_key", "lang", "item_value AS value", "http_code").
		Where("status = ?", 1).
		Scan(&rows).Error
	if err != nil {
		return err
	}

	newData := make(map[string]map[string]string)
	newHttpCodes := make(map[string]int)

	for _, r := range rows {
		if newData[r.ItemKey] == nil {
			newData[r.ItemKey] = make(map[string]string)
		}
		newData[r.ItemKey][r.Lang] = r.Value

		if r.HttpCode != nil && newHttpCodes[r.ItemKey] == 0 {
			newHttpCodes[r.ItemKey] = *r.HttpCode
		}
	}

	cache.Update(newData, newHttpCodes)
	log.Printf("[i18n] Loaded %d keys, %d records, version: %d", len(newData), len(rows), cache.GetVersion())
	return nil
}

// StartAutoRefresh 启动自动刷新
func StartAutoRefresh(interval time.Duration) {
	if interval <= 0 {
		interval = 20 * time.Second
	}

	refreshMu.Lock()
	defer refreshMu.Unlock()

	if refreshCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	refreshCancel = cancel
	refreshWG.Add(1)

	go func() {
		defer refreshWG.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("[i18n] Auto refresh stopped")
				return
			case <-ticker.C:
				if err := LoadCache(); err != nil {
					log.Printf("[i18n] Auto refresh failed: %v", err)
				}
			}
		}
	}()

	log.Printf("[i18n] Auto refresh started, interval: %s", interval)
}

// StopAutoRefresh 停止自动刷新
func StopAutoRefresh() {
	refreshMu.Lock()
	cancel := refreshCancel
	refreshCancel = nil
	refreshMu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	refreshWG.Wait()
}

// ValidateReady 检查 i18n 是否已完成初始化
func ValidateReady() error {
	if !IsInited() {
		return fmt.Errorf("i18n 未初始化")
	}
	return nil
}

// Ready 检查 i18n 组件是否可用
func Ready() error {
	return ValidateReady()
}