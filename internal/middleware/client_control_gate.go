package middleware

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	appdomain "aegis/internal/domain/app"
	devicedomain "aegis/internal/domain/device"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 版本管控的服务端执行点（docs/client-control.md「服务端执行」一节）。
//
// 客户端自报身份的请求头（SDK 的 AegisDevice 随每个请求附带）：
//
//	X-App-Package       包名
//	X-App-Version-Code  版本号
//	X-App-Version       版本名
//	X-App-Cert          签名证书 SHA-256
//	X-Device-Platform / X-Device-Id
//
// X-Device-OS-Version 是系统版本名（如 14），与规则里的 API 级别口径不同，这里不读：
// 系统版本条件在闸门上按未知字段处理，交给 /client-control/check 判定。
//
// 命中停用或强制更新时，网关其余接口一律拒绝，客户端界面上的处理与此无关。
// 没有自报包名的请求（官网服务端、旧版客户端）不经过这里的判定。

const (
	headerAppPackage     = "X-App-Package"
	headerAppVersionCode = "X-App-Version-Code"
	headerAppVersionName = "X-App-Version"
	headerAppCert        = "X-App-Cert"
	headerDevicePlatform = "X-Device-Platform"
	headerDeviceID       = "X-Device-Id"

	// HeaderClientControl 被拦截时带回的结果（block / force_update），客户端据此立即重新校验。
	HeaderClientControl = "X-Aegis-Client-Control"
	// HeaderClientControlVersion 判定所依据的策略版本。
	HeaderClientControlVersion = "X-Aegis-Client-Control-Version"
)

// ClientControlGateService 给出某个请求的判定。applies 为 false 表示该应用未启用管控或关闭了服务端执行。
type ClientControlGateService interface {
	GateDecision(ctx context.Context, appID int64, report appdomain.ClientReport) (decision *appdomain.ClientControlDecision, policyVersion int64, applies bool)
}

// clientControlExemptSuffixes 被停用的客户端仍需访问的接口：读配置、重新校验、下载更新。
var clientControlExemptSuffixes = []string{"/config", "/auth/oauth/callback"}

var clientControlExemptSegments = []string{"/client-control/", "/releases"}

func isClientControlExempt(path string) bool {
	for _, suffix := range clientControlExemptSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	for _, segment := range clientControlExemptSegments {
		if strings.Contains(path, segment) {
			return true
		}
	}
	return false
}

// ClientControlGate 必须挂在 AppGateway 之后：它读 AppGateway 写入的 aegis.app_id。
func ClientControlGate(service ClientControlGateService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil || !strings.HasPrefix(c.Request.URL.Path, gatewayPathPrefix) || isClientControlExempt(c.Request.URL.Path) {
			c.Next()
			return
		}
		packageName := gateHeader(c, headerAppPackage)
		if packageName == "" {
			c.Next()
			return
		}
		value, ok := c.Get("aegis.app_id")
		appID, _ := value.(int64)
		if !ok || appID <= 0 {
			c.Next()
			return
		}
		report := appdomain.ClientReport{
			PackageName: packageName,
			VersionName: gateHeader(c, headerAppVersionName),
			Platform:    gateHeader(c, headerDevicePlatform),
			CertSHA256:  gateHeader(c, headerAppCert),
			DeviceID:    devicedomain.NormalizeID(gateHeader(c, headerDeviceID)),
			IP:          c.ClientIP(),
		}
		report.VersionCode, _ = strconv.ParseInt(gateHeader(c, headerAppVersionCode), 10, 64)

		decision, version, applies := service.GateDecision(c.Request.Context(), appID, report)
		if !applies || decision == nil || !appdomain.ClientControlActionBlocks(decision.Action) {
			c.Next()
			return
		}
		code := 40329
		if decision.Action == appdomain.ClientControlActionForceUpdate {
			code = 40326
		}
		c.Header(HeaderClientControl, decision.Action)
		c.Header(HeaderClientControlVersion, strconv.FormatInt(version, 10))
		message := decision.Message
		if message == "" {
			message = decision.Title
		}
		response.Error(c, http.StatusForbidden, code, message)
		c.Abort()
	}
}

// gateHeader 读请求头；SDK 对非 ASCII 值做了百分号编码，这里解回来。
func gateHeader(c *gin.Context, name string) string {
	value := strings.TrimSpace(c.GetHeader(name))
	if strings.Contains(value, "%") {
		if decoded, err := url.PathUnescape(value); err == nil {
			value = decoded
		}
	}
	return value
}
