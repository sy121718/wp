package uploadprovider

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// MediaBaseURL 解析可信配置中的媒体基址。上传与附件读取共用同一条规则。
// 生产必须显式提供完整地址；本地开发只按配置端口使用环回地址，不采信请求 Host。
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
