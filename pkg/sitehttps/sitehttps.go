package sitehttps

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

const siteHTTPSKey = "server.site_https"

var (
	cfg *viper.Viper
)

func Init(v *viper.Viper) {
	cfg = v
}

func Enabled() bool {
	if cfg == nil {

		return gin.Mode() == gin.ReleaseMode
	}
	if cfg.IsSet(siteHTTPSKey) {
		return cfg.GetBool(siteHTTPSKey)
	}
	return strings.EqualFold(strings.TrimSpace(cfg.GetString("server.mode")), "release")
}

func ResetForTest() {
	cfg = nil
}
