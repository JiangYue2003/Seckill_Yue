package mq

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// AsyncProducer 异步消息生产者
// 核心设计：将 MQ 投递从用户请求的关键路径（critical path）中移除，
// 通过带缓冲的 Channel 吸收突发流量，后台 Worker Pool 负责异步投递和重试。
type AsyncProducer struct {
	producer    *Producer                 // 底层同步生产者
	bufferSize  int                       // Channel 缓冲大小
	workerCount int                       // 后台 Worker 数量
	retryCount  int                       // 最大重试次数
	retryBase   time.Duration             // 基础重试间隔（指数退避的起点）
	channel     chan *SeckillOrderMessage // 缓冲队列
	done        chan struct{}             // 关闭信号
	wg          sync.WaitGroup            // 等待所有 Worker 退出
}

// NewAsyncProducer 创建异步生产者
// producer: 底层同步 RabbitMQ 生产者（由调用方保证已初始化）
// bufferSize: Channel 缓冲队列大小，建议设为预估 TPS 的 5-10 倍
// workerCount: 后台 Worker 协程数量，建议设为 CPU 核数的 2-4 倍
// retryCount: 最大重试次数，三次失败后消息丢弃，依赖 TTL 兜底
func NewAsyncProducer(producer *Producer, bufferSize, workerCount, retryCount, retryIntervalSec int) *AsyncProducer {
	if bufferSize <= 0 {
		bufferSize = 10000
	}
	if workerCount <= 0 {
		workerCount = 4
	}
	if retryCount <= 0 {
		retryCount = 3
	}
	if retryIntervalSec <= 0 {
		retryIntervalSec = 1
	}

	p := &AsyncProducer{
		producer:    producer,
		bufferSize:  bufferSize,
		workerCount: workerCount,
		retryCount:  retryCount,
		retryBase:   time.Duration(retryIntervalSec) * time.Second,
		channel:     make(chan *SeckillOrderMessage, bufferSize),
		done:        make(chan struct{}),
	}

	// 启动后台 Worker Pool
	for i := 0; i < workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	logx.Infof("AsyncProducer started: bufferSize=%d, workerCount=%d, retryCount=%d",
		bufferSize, workerCount, retryCount)

	return p
}

// SendDelayOrder 异步发送延迟兜底消息（写入 buffer channel，不阻塞关键路径）
func (p *AsyncProducer) SendDelayOrder(ctx context.Context, msg *SeckillOrderMessage) error {
	// 复制一份，打上 delay 标记，避免修改原始消息
	delayMsg := *msg
	delayMsg.IsDelay = true
	select {
	case p.channel <- &delayMsg:
		return nil
	default:
		logx.Errorf("AsyncProducer delay buffer full, message dropped: orderId=%s", msg.OrderId)
		return &BufferFullError{OrderId: msg.OrderId, BufferSize: p.bufferSize}
	}
}

// SendAsync 异步投递消息，立即返回不阻塞
// 返回值:
//   - nil: 消息已写入缓冲区，由后台 Worker 负责最终投递
//   - error: 缓冲区已满（积压过多），调用方应降级处理（如回滚库存）
func (p *AsyncProducer) SendAsync(ctx context.Context, msg *SeckillOrderMessage) error {
	select {
	case p.channel <- msg:
		return nil
	default:
		// 缓冲区满，消息积压超过阈值，降级处理
		logx.Errorf("AsyncProducer buffer full, message dropped: orderId=%s, bufferSize=%d",
			msg.OrderId, p.bufferSize)
		return &BufferFullError{OrderId: msg.OrderId, BufferSize: p.bufferSize}
	}
}

// worker 后台协程：持续从 Channel 消费并投递到 RabbitMQ
// 指数退避重试策略：1s → 3s → 10s → ...（retryBase * 2^attempt）
// 三次失败后记录错误日志，消息丢弃（依赖 Lua TTL 兜底机制自然回收库存）
func (p *AsyncProducer) worker(id int) {
	defer p.wg.Done()

	logx.Infof("AsyncProducer worker-%d started", id)

	for {
		select {
		case <-p.done:
			// 收到关闭信号 Drain 模式：处理完 Channel 中剩余消息后再退出
			logx.Infof("AsyncProducer worker-%d draining remaining messages...", id)
			for {
				select {
				case msg := <-p.channel:
					p.sendWithRetry(msg)
				default:
					logx.Infof("AsyncProducer worker-%d drained and stopped", id)
					return
				}
			}
		case msg := <-p.channel:
			p.sendWithRetry(msg)
		}
	}
}

// sendWithRetry 投递消息，带指数退避重试
func (p *AsyncProducer) sendWithRetry(msg *SeckillOrderMessage) {
	var lastErr error

	for attempt := 0; attempt < p.retryCount; attempt++ {
		if attempt > 0 {
			// 指数退避：retryBase * 2^(attempt-1)，即 1s, 2s, 4s...（上限 10s）
			backoff := p.retryBase * time.Duration(1<<uint(attempt-1))
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
			logx.Infof("AsyncProducer retrying: orderId=%s, attempt=%d/%d, backoff=%v",
				msg.OrderId, attempt+1, p.retryCount, backoff)
			time.Sleep(backoff)
		}

		if err := func() error {
			if msg.IsDelay {
				return p.producer.SendDelayOrder(context.Background(), msg)
			}
			return p.producer.SendSeckillOrder(context.Background(), msg)
		}(); err != nil {
			lastErr = err
			logx.Errorf("AsyncProducer send failed: orderId=%s, attempt=%d/%d, err=%v",
				msg.OrderId, attempt+1, p.retryCount, err)
			continue
		}

		// 投递成功
		logx.Infof("AsyncProducer send success: orderId=%s, attempt=%d", msg.OrderId, attempt+1)
		return
	}

	// 三次重试全部失败，消息丢弃
	// 此处不触发库存回滚（避免雪崩），依赖 UserPreemptTTL（300s）自然过期回收库存
	logx.Errorf("AsyncProducer message dropped after max retries: orderId=%s, userId=%d, seckillProductId=%d, lastErr=%v",
		msg.OrderId, msg.UserId, msg.SeckillProductId, lastErr)
}

// Close 优雅关闭
// 1. 发送关闭信号，所有 Worker 切换到 Drain 模式
// 2. 等待 Worker 处理完 Channel 中剩余消息
// 3. 关闭底层 RabbitMQ 连接
func (p *AsyncProducer) Close() error {
	logx.Info("AsyncProducer closing...")

	// 1. 发送关闭信号（发送一次即可，所有 Worker 都会收到）
	close(p.done)

	// 2. 等待所有 Worker 退出（Drain 完成后才退出）
	p.wg.Wait()

	// 3. 关闭底层 RabbitMQ 连接
	if err := p.producer.Close(); err != nil {
		logx.Errorf("AsyncProducer failed to close underlying producer: %v", err)
		return err
	}

	logx.Info("AsyncProducer closed gracefully")
	return nil
}

// BufferFullError 缓冲区满错误
type BufferFullError struct {
	OrderId    string
	BufferSize int
}

func (e *BufferFullError) Error() string {
	return "async producer buffer full: orderId=" + e.OrderId + ", bufferSize=" + strconv.Itoa(e.BufferSize)
}
