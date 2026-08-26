package i18n

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go_wp/pkg/database"
	"go_wp/pkg/logger"
)

var (
	refreshMu     sync.Mutex
	refreshCancel context.CancelFunc
	refreshWG     sync.WaitGroup
)

// LoadCache 从数据库加载多语言数据到内存
func LoadCache() error {
	db, err := database.GetDB()
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
	logger.WithFields(map[string]any{
		"keys":    len(newData),
		"records": len(rows),
		"version": cache.GetVersion(),
	}).Info("i18n 缓存加载完成")
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
				logger.Info("i18n 自动刷新已停止")
				return
			case <-ticker.C:
				if err := LoadCache(); err != nil {
					logger.Error(err, "i18n 自动刷新失败")
				}
			}
		}
	}()

	logger.WithFields(map[string]any{"interval": interval.String()}).Info("i18n 自动刷新已启动")
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

// ValidateReady 检查 i18n 是否已经完成初始化
func ValidateReady() error {
	if !IsInited() {
		return fmt.Errorf("i18n 未初始化")
	}
	return nil
}

// Ready 检查 i18n 组件是否可用。
func Ready() error {
	return ValidateReady()
}
