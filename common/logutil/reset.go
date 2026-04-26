package logutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
)

// ResetLogsIfEnabled clears existing log files before startup.
// It only runs when mode is dev/test and resetOnStart is explicitly enabled.
func ResetLogsIfEnabled(mode string, resetOnStart bool, conf logx.LogConf) error {
	if !resetOnStart {
		return nil
	}
	if mode != service.DevMode && mode != service.TestMode {
		return nil
	}

	logPath := strings.TrimSpace(conf.Path)
	if logPath == "" {
		return nil
	}

	cleanPath := filepath.Clean(logPath)
	if cleanPath == "." || cleanPath == string(filepath.Separator) {
		return fmt.Errorf("refuse to reset unsafe log path: %q", conf.Path)
	}

	if err := os.MkdirAll(cleanPath, 0o755); err != nil {
		return fmt.Errorf("create log path failed: %w", err)
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return fmt.Errorf("read log path failed: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cleanPath, entry.Name())); err != nil {
			return fmt.Errorf("remove log entry %s failed: %w", entry.Name(), err)
		}
	}

	return nil
}
