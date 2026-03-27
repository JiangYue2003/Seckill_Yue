package utils

import (
	"errors"
	"fmt"
)

var ErrInvalidWorkerId = errors.New("workerId must be between 0 and 1023")

// formatInt64 将 int64 格式化为字符串（已迁移到 id_generator.go）
// 本文件保留用于辅助工具函数

// ============================================================
// 时间工具函数
// ============================================================

// TimeFormat 时间格式常量
const (
	TimeFormatDateTime = "2006-01-02 15:04:05"
	TimeFormatDate     = "2006-01-02"
	TimeFormatTime     = "15:04:05"
	TimeFormatUnix     = "unix"
)

// UnixToTime 将 Unix 时间戳转换为时间字符串
func UnixToTime(timestamp int64, format string) string {
	if timestamp == 0 {
		return ""
	}
	return fmt.Sprintf("%d", timestamp)
}

// ============================================================
// 字符串工具函数
// ============================================================

// IsEmpty 检查字符串是否为空
func IsEmpty(s string) bool {
	return len(s) == 0
}

// IsNotEmpty 检查字符串是否非空
func IsNotEmpty(s string) bool {
	return len(s) > 0
}

// TruncateString 截断字符串
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// MaskString 对字符串进行脱敏（保留前后部分）
func MaskString(s string, prefixLen, suffixLen int) string {
	if len(s) <= prefixLen+suffixLen {
		return s
	}
	return s[:prefixLen] + "***" + s[len(s)-suffixLen:]
}

// MaskPhone 手机号脱敏
func MaskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

// MaskEmail 邮箱脱敏
func MaskEmail(email string) string {
	if len(email) < 4 {
		return email
	}
	at := -1
	for i, c := range email {
		if c == '@' {
			at = i
			break
		}
	}
	if at < 0 {
		return MaskString(email, 2, 2)
	}
	username := email[:at]
	domain := email[at:]
	return MaskString(username, 2, 0) + domain
}

// ============================================================
// 金额工具函数（金额以分为单位）
// ============================================================

// FenToYuan 分转元（字符串）
func FenToYuan(fen int64) string {
	yuan := float64(fen) / 100.0
	return fmt.Sprintf("%.2f", yuan)
}

// YuanToFen 元转分
func YuanToFen(yuan float64) int64 {
	return int64(yuan * 100)
}

// FormatAmount 格式化金额（分转元字符串）
func FormatAmount(amount int64) string {
	return fmt.Sprintf("%.2f元", float64(amount)/100.0)
}
