package feature

import (
	"os"
	"path/filepath"
)

// browserPuppeteerModule 让浏览器用例解析到本机安装的 puppeteer-core：
// 优先 GOWP_PUPPETEER_MODULE，否则回落到桌面端 profile 下的依赖。
// 显式传给子进程，避免依赖调用方 shell 是否导出该变量。
func browserPuppeteerModule() string {
	if v := os.Getenv("GOWP_PUPPETEER_MODULE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "puppeteer-core"
	}
	return filepath.Join(home, ".dsh", "profiles", "desktop", "node_modules", "puppeteer-core")
}
