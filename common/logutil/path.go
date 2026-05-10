package logutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/zeromicro/go-zero/core/logx"
)

// BuildLogPath 构建实例专属日志路径，支持三种部署场景：
//   - 单机单实例：port=0, withHostname=false → basePath 不变
//   - 单机多实例：port>0, withHostname=false → basePath/{port}
//   - 多物理机多实例：port>0, withHostname=true → basePath/{hostname}/{port}
func BuildLogPath(basePath string, port int, withHostname bool) string {
	if !withHostname && port <= 0 {
		return basePath
	}
	parts := []string{basePath}
	if withHostname {
		if h, err := os.Hostname(); err == nil && h != "" {
			parts = append(parts, h)
		} else {
			parts = append(parts, "unknown")
		}
	}
	if port > 0 {
		parts = append(parts, strconv.Itoa(port))
	}
	return filepath.Join(parts...)
}

// SetupInstanceFields 在 logx.MustSetup 之后调用，向所有日志注入实例标识字段。
// 每条日志都会携带 service/hostname/port/instance 字段，
// 方便在 Elasticsearch/Kibana 中按实例过滤和追踪。
func SetupInstanceFields(serviceName string, port int) {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}

	fields := []logx.LogField{
		logx.Field("service", serviceName),
		logx.Field("hostname", hostname),
	}
	if port > 0 {
		fields = append(fields,
			logx.Field("port", port),
			logx.Field("instance", fmt.Sprintf("%s:%d", hostname, port)),
		)
	}
	logx.AddGlobalFields(fields...)
}
