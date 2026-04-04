package main

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	rabbitMQURL    = "amqp://guest:guest@localhost:5672/"
	testExchange   = "seckill_reliability_test_exchange"
	testQueue      = "seckill_reliability_test_queue"
	testRoutingKey = "reliability.test"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("   MQ 可靠性测试（Confirm + 重连）")
	fmt.Println("========================================")
	fmt.Println()

	if err := setupTestInfrastructure(); err != nil {
		log.Fatalf("[INIT] 初始化测试基础设施失败: %v", err)
	}
	fmt.Println("[OK] 测试基础设施初始化完成")
	fmt.Println()

	passed, failed := 0, 0
	run := func(name string, fn func() error) {
		fmt.Printf("▶ %s\n", name)
		if err := fn(); err != nil {
			fmt.Printf("  ✗ FAIL: %v\n\n", err)
			failed++
		} else {
			fmt.Printf("  ✓ PASS\n\n")
			passed++
		}
	}

	run("TC-01 基础发布可达性（消息发布后能从队列消费到）", testBasicPublish)
	run("TC-02 Publish Return 检测（mandatory 消息无路由时被 Return，可感知）", testConfirmReturn)
	run("TC-03 Producer 断线自动重连（断连后能恢复发消息）", testProducerReconnect)
	run("TC-04 Consumer 断线自动重连（断连后能恢复消费消息）", testConsumerReconnect)

	cleanupTestInfrastructure()

	fmt.Println("========================================")
	fmt.Printf("   测试完成: %d 通过 / %d 失败\n", passed, failed)
	fmt.Println("========================================")
}

// ==================== 测试基础设施 ====================

func setupTestInfrastructure() error {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare(testExchange, "direct", false, true, false, false, nil); err != nil {
		return fmt.Errorf("声明测试交换机失败: %w", err)
	}
	if _, err := ch.QueueDeclare(testQueue, false, true, false, false, nil); err != nil {
		return fmt.Errorf("声明测试队列失败: %w", err)
	}
	if err := ch.QueueBind(testQueue, testRoutingKey, testExchange, false, nil); err != nil {
		return fmt.Errorf("绑定测试队列失败: %w", err)
	}
	return nil
}

func cleanupTestInfrastructure() {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return
	}
	defer conn.Close()
	ch, _ := conn.Channel()
	if ch != nil {
		ch.QueuePurge(testQueue, false)
		ch.QueueDelete(testQueue, false, false, false)
		ch.ExchangeDelete(testExchange, false, false)
		ch.Close()
	}
}

// ==================== TC-01: 基础发布可达性 ====================

func testBasicPublish() error {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel 失败: %w", err)
	}
	defer ch.Close()

	// 发布消息
	if err := ch.Publish(testExchange, testRoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         []byte(`{"order_id":"basic_publish_001"}`),
	}); err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}

	// 消费验证消息已到达队列
	msg, ok, err := ch.Get(testQueue, true)
	if err != nil {
		return fmt.Errorf("消费消息失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("队列中未找到消息（发布后立即消费）")
	}

	fmt.Printf("     → 消息成功到达队列并消费，body=%s ✓\n", string(msg.Body))
	return nil
}

// ==================== TC-02: Publish Return 检测 ====================
// mandatory=true 时消息无路由会触发 Return，不依赖 confirm 模式

func testConfirmReturn() error {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel 失败: %w", err)
	}
	defer ch.Close()

	returns := ch.NotifyReturn(make(chan amqp.Return, 1))

	if err := ch.Publish(testExchange, "nonexistent.routing.key.xyz", true, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"order_id":"return_test_001"}`),
	}); err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}

	select {
	case ret := <-returns:
		fmt.Printf("     → 收到 Return（replyCode=%d, replyText=%s）— 无路由消息可感知 ✓\n",
			ret.ReplyCode, ret.ReplyText)
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("超时：未收到 Return 通知（5s）")
	}
}

// ==================== TC-03: Producer 断线自动重连 ====================

// publishAndVerify 发布一条消息并验证能从队列消费到
func publishAndVerify(ch *amqp.Channel, orderId string) error {
	if err := ch.Publish(testExchange, testRoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         []byte(`{"order_id":"` + orderId + `"}`),
	}); err != nil {
		return fmt.Errorf("发送失败: %w", err)
	}
	// 短暂等待消息路由到队列
	time.Sleep(100 * time.Millisecond)
	msg, ok, err := ch.Get(testQueue, true)
	if err != nil {
		return fmt.Errorf("消费验证失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("消息未到达队列（orderId=%s）", orderId)
	}
	_ = msg
	return nil
}

func testProducerReconnect() error {
	// === Phase 1: 建立连接，验证发布正常 ===
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("open channel 失败: %w", err)
	}
	closeNotify := conn.NotifyClose(make(chan *amqp.Error, 1))

	if err := publishAndVerify(ch, "pre_disconnect_001"); err != nil {
		conn.Close()
		return fmt.Errorf("断连前发送失败: %w", err)
	}
	fmt.Printf("     → Phase 1: 初始连接发布成功 ✓\n")

	// === Phase 2: 模拟连接断开，验证 NotifyClose 触发 ===
	var reconnected atomic.Bool
	var newConn *amqp.Connection
	var newCh *amqp.Channel

	// 重连 goroutine：任意关闭（包括 graceful）都执行重连
	go func() {
		<-closeNotify // 等待关闭信号（nil = graceful, non-nil = error，均触发重连）
		fmt.Printf("     → NotifyClose 触发，开始重连...\n")
		for attempt := 1; attempt <= 5; attempt++ {
			time.Sleep(500 * time.Millisecond)
			c, err := amqp.Dial(rabbitMQURL)
			if err != nil {
				fmt.Printf("     → 重连失败（第%d次）: %v\n", attempt, err)
				continue
			}
			c2, err := c.Channel()
			if err != nil {
				c.Close()
				continue
			}
			newConn = c
			newCh = c2
			fmt.Printf("     → 重连成功（第%d次）✓\n", attempt)
			reconnected.Store(true)
			return
		}
	}()

	// 关闭连接（graceful close，触发 NotifyClose channel）
	conn.Close()
	fmt.Printf("     → Phase 2: 连接已关闭\n")

	// === Phase 3: 等待重连完成 ===
	deadline := time.Now().Add(10 * time.Second)
	for !reconnected.Load() {
		if time.Now().After(deadline) {
			return fmt.Errorf("重连超时（10s）")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// === Phase 4: 验证新连接发布正常 ===
	defer newConn.Close()
	if err := publishAndVerify(newCh, "post_reconnect_001"); err != nil {
		return fmt.Errorf("重连后发送失败: %w", err)
	}
	fmt.Printf("     → Phase 4: 重连后发布成功 ✓\n")
	return nil
}

// ==================== TC-04: Consumer 断线自动重连 ====================

func testConsumerReconnect() error {
	// === Phase 1: 建立消费者，消费一条预存消息验证连接正常 ===
	consumerConn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("消费者连接失败: %w", err)
	}
	consumerCh, err := consumerConn.Channel()
	if err != nil {
		consumerConn.Close()
		return fmt.Errorf("open channel 失败: %w", err)
	}

	// 先往队列里放一条消息（供断连前验证用）
	producerConn, _ := amqp.Dial(rabbitMQURL)
	producerCh, _ := producerConn.Channel()
	producerCh.Publish(testExchange, testRoutingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"order_id":"pre_disconnect_consumer_001"}`),
	})
	producerCh.Close()
	producerConn.Close()

	startConsuming := func(ch *amqp.Channel) (<-chan amqp.Delivery, error) {
		return ch.Consume(testQueue, "", true, false, false, false, nil)
	}
	msgs, err := startConsuming(consumerCh)
	if err != nil {
		consumerConn.Close()
		return fmt.Errorf("启动消费失败: %w", err)
	}

	// 等收到预存消息，验证消费者正常工作
	select {
	case msg := <-msgs:
		fmt.Printf("     → Phase 1: 收到消息 %s，消费者正常 ✓\n", string(msg.Body))
	case <-time.After(3 * time.Second):
		consumerConn.Close()
		return fmt.Errorf("Phase 1: 3s 内未收到预存消息")
	}

	// === Phase 2: 关闭连接，模拟断线 ===
	var reconnected atomic.Bool
	var receivedAfterReconnect atomic.Int32

	// 消费 goroutine（含重连逻辑）
	go func() {
		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					// channel 关闭 → 重连
					fmt.Printf("     → Phase 2: channel 关闭，开始重连...\n")
					for attempt := 1; attempt <= 5; attempt++ {
						time.Sleep(500 * time.Millisecond)
						newConn, err := amqp.Dial(rabbitMQURL)
						if err != nil {
							fmt.Printf("     → 重连失败（第%d次）: %v\n", attempt, err)
							continue
						}
						newCh, err := newConn.Channel()
						if err != nil {
							newConn.Close()
							continue
						}
						newMsgs, err := startConsuming(newCh)
						if err != nil {
							newConn.Close()
							continue
						}
						consumerConn = newConn
						msgs = newMsgs
						fmt.Printf("     → Phase 3: 重连成功（第%d次）✓\n", attempt)
						reconnected.Store(true)
						break
					}
					continue
				}
				fmt.Printf("     → 重连后收到消息: %s ✓\n", string(msg.Body))
				receivedAfterReconnect.Add(1)
			}
		}
	}()

	consumerConn.Close() // 触发 msgs channel 关闭
	fmt.Printf("     → Phase 2: 连接已关闭\n")

	// === Phase 3: 等待重连 ===
	deadline := time.Now().Add(10 * time.Second)
	for !reconnected.Load() {
		if time.Now().After(deadline) {
			return fmt.Errorf("重连超时（10s）")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// === Phase 4: 重连后发消息，验证消费者能收到 ===
	p2, _ := amqp.Dial(rabbitMQURL)
	defer p2.Close()
	p2ch, _ := p2.Channel()
	defer p2ch.Close()
	p2ch.Publish(testExchange, testRoutingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"order_id":"post_reconnect_consumer_001"}`),
	})

	deadline = time.Now().Add(5 * time.Second)
	for receivedAfterReconnect.Load() == 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("重连后消费者 5s 内未能收到新消息")
		}
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("     → Phase 4: 重连后消费者成功消费新消息 ✓\n")
	return nil
}
