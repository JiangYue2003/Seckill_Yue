package utils

import (
	"sync"
	"time"
)

// ============================================================
// Snowflake ID Generator - 雪花算法分布式唯一 ID 生成器
// ============================================================
// 原理：64 位整数 = 时间戳(41bit) + 机器 ID(10bit) + 序列号(12bit)
// 优点：趋势递增、不依赖数据库、高性能
// ============================================================

const (
	// 时间戳起点（2024-01-01 00:00:00 UTC），可根据实际情况调整
	epoch          = 1704067200000
	workerIdBits   = 10                          // 机器 ID 位数
	sequenceBits   = 12                          // 序列号位数
	workerIdShift  = sequenceBits                // 机器 ID 左移位数
	timestampShift = sequenceBits + workerIdBits // 时间戳左移位数
	sequenceMask   = -1 ^ (-1 << sequenceBits)   // 序列号掩码
	maxWorkerId    = -1 ^ (-1 << workerIdBits)   // 最大机器 ID
)

// Snowflake 雪花算法实例
type Snowflake struct {
	mu       sync.Mutex
	workerId int64 // 机器 ID (0 ~ 1023)
	sequence int64 // 序列号
	lastTime int64 // 上次生成 ID 的时间戳
}

// NewSnowflake 创建雪花算法生成器
// workerId: 机器 ID，必须在 0 ~ 1023 之间
func NewSnowflake(workerId int64) (*Snowflake, error) {
	if workerId < 0 || workerId > maxWorkerId {
		return nil, ErrInvalidWorkerId
	}
	return &Snowflake{
		workerId: workerId,
		sequence: 0,
		lastTime: 0,
	}, nil
}

// NewSnowflakeDefault 创建默认雪花算法生成器（workerId = 0）
// 仅用于单机场景，生产环境请指定唯一的 workerId
func NewSnowflakeDefault() *Snowflake {
	return &Snowflake{
		workerId: 0,
		sequence: 0,
		lastTime: 0,
	}
}

// NextId 生成下一个唯一 ID
// 返回 int64 类型的唯一 ID
func (s *Snowflake) NextId() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixNano() / 1e6 // 毫秒时间戳

	// 时间回拨检测：如果当前时间小于上次生成时间，说明时钟回拨
	if now < s.lastTime {
		// 等待追上
		now = s.waitUntilNextMillis(s.lastTime)
	}

	// 同一毫秒内，序列号递增
	if now == s.lastTime {
		s.sequence = (s.sequence + 1) & sequenceMask
		// 序列号溢出，等待下一毫秒
		if s.sequence == 0 {
			now = s.waitUntilNextMillis(s.lastTime)
		}
	} else {
		// 新的一毫秒，序列号重置
		s.sequence = 0
	}

	s.lastTime = now

	// 组装 ID
	// ((now - epoch) << timestampShift) | (workerId << workerIdShift) | sequence
	id := (now-epoch)<<timestampShift | s.workerId<<workerIdShift | s.sequence
	return id
}

// waitUntilNextMillis 等待直到下一毫秒
func (s *Snowflake) waitUntilNextMillis(lastTime int64) int64 {
	now := time.Now().UnixNano() / 1e6
	for now <= lastTime {
		time.Sleep(time.Microsecond * 100)
		now = time.Now().UnixNano() / 1e6
	}
	return now
}

// ParseId 解析 ID 的组成部分（用于调试）
func (s *Snowflake) ParseId(id int64) (timestamp int64, workerId int64, sequence int64) {
	timestamp = (id >> timestampShift) + epoch
	workerId = (id >> workerIdShift) & maxWorkerId
	sequence = id & sequenceMask
	return
}

// ============================================================
// 订单号生成（基于雪花算法）
// ============================================================

// OrderIdPrefix 订单号前缀
var OrderIdPrefix = map[string]string{
	"NORMAL":  "N", // 普通订单
	"SECKILL": "S", // 秒杀订单
}

// GenerateOrderId 生成订单号
// prefix: 订单前缀（N=普通，S=秒杀）
// 返回格式: {prefix}{雪花ID}
func GenerateOrderId(prefix string) string {
	sf := GetSnowflake()
	orderId := sf.NextId()
	return prefix + Int64ToString(orderId)
}

// GetSnowflake 获取全局雪花算法实例（线程安全）
// 注意：生产环境应使用单例模式，避免重复创建
var globalSnowflake *Snowflake
var snowflakeOnce sync.Once

func GetSnowflake() *Snowflake {
	snowflakeOnce.Do(func() {
		globalSnowflake, _ = NewSnowflake(0)
	})
	return globalSnowflake
}

// ============================================================
// 辅助函数
// ============================================================

// Int64ToString int64 转字符串
func Int64ToString(n int64) string {
	return formatInt64(n)
}

func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}

	var result []byte
	negative := n < 0
	if negative {
		n = -n
	}

	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}

	if negative {
		result = append([]byte{'-'}, result...)
	}

	return string(result)
}

// StringToInt64 字符串转 int64
func StringToInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			continue
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// BytesToString bytes 转字符串
func BytesToString(b []byte) string {
	return string(b)
}

// StringToBytes 字符串转 bytes
func StringToBytes(s string) []byte {
	return []byte(s)
}
