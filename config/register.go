package config

import (
	internaltask "go_wp/internal/task"
	"go_wp/pkg/auth"
	"go_wp/pkg/cache"
	"go_wp/pkg/captcha"
	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	pkglogger "go_wp/pkg/logger"
	"go_wp/pkg/queue"
	"go_wp/pkg/upload"
	pkgvalidate "go_wp/pkg/validate"

	"github.com/spf13/viper"
)

type runtimeComponent struct {
	Name     string
	Critical bool
	Enabled  func(cfg *viper.Viper) bool
	Init     func(cfg *viper.Viper) error
	Ready    func() error
	Close    func() error
}

var runtimePreparers = []func(){
	internaltask.RegisterHandlers,
}

var runtimeComponents = []runtimeComponent{
	{
		Name:     "logger",
		Critical: true,
		Init:     pkglogger.Init,
		Ready:    pkglogger.Ready,
		Close:    pkglogger.Close,
	},
	{
		Name:     "validate",
		Critical: true,
		Init: func(_ *viper.Viper) error {
			return pkgvalidate.RegisterCustomRules()
		},
	},
	{
		Name:     "database",
		Critical: true,
		Init:     database.Init,
		Ready:    database.Ready,
		Close:    database.Close,
	},
	{
		Name: "casbin",
		Enabled: func(cfg *viper.Viper) bool {
			return cfg.GetBool("casbin.enabled")
		},
		Init:  casbin.Init,
		Ready: casbin.Ready,
		Close: casbin.Close,
	},
	{
		Name:     "i18n",
		Critical: true,
		Init: func(cfg *viper.Viper) error {
			return i18n.Init(cfg)
		},
		Ready: i18n.Ready,
		Close: i18n.Close,
	},
	{
		// H3：认证会话/封禁/心跳硬依赖 cache（Redis）。
		// 升级为 Critical 并置于 auth 之前（InitComponents 按 slice 顺序初始化 Critical 组件）：
		// redis.enabled=true 时先就绪 cache，auth 组件 Init 阶段的 RequireSessionStorage 才能通过；
		// redis.enabled=false 时本组件跳过，由 auth 组件 Init 的 RequireSessionStorage fail-fast 终止启动。
		Name:     "cache",
		Critical: true,
		Enabled: func(cfg *viper.Viper) bool {
			return cfg.GetBool("redis.enabled")
		},
		Init:  cache.Init,
		Ready: cache.Ready,
		Close: cache.Close,
	},
	{
		Name:     "auth",
		Critical: true,
		Init: func(cfg *viper.Viper) error {
			if err := auth.Init(cfg); err != nil {
				return err
			}
			// H3 fail-fast：会话后端存储必须就绪，避免「启动正常、登录后全站 503」。
			return auth.RequireSessionStorage()
		},
		Ready: auth.Ready,
		Close: auth.Close,
	},
	{
		Name: "upload",
		Enabled: func(cfg *viper.Viper) bool {
			return cfg.GetBool("upload.enabled")
		},
		Init:  upload.Init,
		Ready: upload.Ready,
		Close: upload.Close,
	},
	{
		Name: "queue",
		Enabled: func(cfg *viper.Viper) bool {
			return cfg.GetBool("queue.enabled")
		},
		Init:  queue.Init,
		Ready: queue.Ready,
		Close: queue.Close,
	},
	{
		Name:     "captcha",
		Critical: false,
		Init: func(cfg *viper.Viper) error {
			captcha.Init(&captcha.Config{
				Length:     cfg.GetInt("captcha.length"),
				ExpireTime: cfg.GetDuration("captcha.expire_time"),
				Width:      cfg.GetInt("captcha.width"),
				Height:     cfg.GetInt("captcha.height"),
			})
			return nil
		},
		Close: captcha.Close,
	},
}
