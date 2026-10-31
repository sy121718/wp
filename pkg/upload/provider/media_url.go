package uploadprovider

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

func MediaBaseURL(v *viper.Viper) (string, error) {
	if v == nil {
		return "", fmt.Errorf("媒体地址配置为空")
	}
	raw := strings.TrimSpace(v.GetString("upload.base_url"))
	if raw == "" {
		mode := strings.ToLower(strings.TrimSpace(v.GetString("server.mode")))
		if mode != "debug" && mode != "test" {
			return "", fmt.Errorf("upload.base_url 必须配置完整的 HTTP(S) 地址")
		}
		port := v.GetInt("server.port")
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("未配置 upload.base_url 时，server.port 必须是有效端口")
		}
		raw = "http://127.0.0.1:" + strconv.Itoa(port)
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\ \t\n\r") {
		return "", fmt.Errorf("upload.base_url 必须是无用户信息、查询串和锚点的完整 HTTP(S) 地址")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/storage") {
		u.Path += "/storage"
	}
	u.RawPath = ""
	return u.String(), nil
}
