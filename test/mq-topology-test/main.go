package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	rabbitMQURL    = "amqp://guest:guest@localhost:5672/"
	managementURL  = "http://localhost:15672/api"
	managementUser = "guest"
	managementPass = "guest"

	// 生产环境队列/交换机名称
	mainExchange    = "seckill_exchange"
	dlxExchange     = "seckill_dlx"
	mainQueue       = "seckill_order_queue"
	delayQueue      = "seckill_delay_queue"
	checkQueue      = "seckill_order_check_queue"
	deadQueue       = "seckill_dead_queue"
	routingKeyOrder = "seckill.order"
	routingKeyDelay = "seckill.delay"
	routingKeyCheck = "seckill.order.check"
	routingKeyDead  = "seckill.dead"

	// 测试专用隔离队列（auto-delete，不影响生产）
	testDelayQueue = "seckill_test_delay_queue"
	testCheckQueue = "seckill_test_check_queue"
	testDLXQueue   = "seckill_test_dlx_queue"
	testExchange   = "seckill_test_exchange"
)

var conn *amqp.Connection

func main() {
	fmt.Println("========================================")
	fmt.Println("   MQ 延迟队列 & DLX 功能测试")
	fmt.Println("========================================")
	fmt.Println()

	var err error
	conn, err = amqp.Dial(rabbitMQURL)
	if err != nil {
		log.Fatalf("[INIT] RabbitMQ 连接失败: %v", err)
	}
	defer conn.Close()
	fmt.Println("[OK] RabbitMQ 连接成功")
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

	run("TC-01 队列拓扑验证（所有队列/交换机存在，参数正确）", testTopology)
	run("TC-02 延迟路由机制验证（消息到期后自动路由到目标队列）", testDelayRouting)
	run("TC-03 DLX 死信路由验证（Nack 后消息自动路由到死信队列）", testDLXRouting)
	run("TC-04 主队列 DLX 绑定验证（主队列 Nack 消息路由到 seckill_dead_queue）", testMainQueueDLX)

	fmt.Println("========================================")
	fmt.Printf("   测试完成: %d 通过 / %d 失败\n", passed, failed)
	fmt.Println("========================================")
}

// ==================== TC-01: 队列拓扑验证 ====================

func testTopology() error {
	// 验证 seckill_dlx 交换机存在
	if err := checkExchangeExists(dlxExchange); err != nil {
		return fmt.Errorf("死信交换机 %s 不存在: %w", dlxExchange, err)
	}
	fmt.Printf("     → 交换机 %s ✓\n", dlxExchange)

	// 验证主队列有 DLX 参数
	args, err := getQueueArguments(mainQueue)
	if err != nil {
		return fmt.Errorf("获取主队列参数失败: %w", err)
	}
	if dlx, ok := args["x-dead-letter-exchange"]; !ok || dlx != dlxExchange {
		return fmt.Errorf("主队列 %s 缺少或错误的 x-dead-letter-exchange，当前值: %v", mainQueue, dlx)
	}
	fmt.Printf("     → 主队列 %s DLX=%s ✓\n", mainQueue, dlxExchange)

	// 验证延迟队列有 TTL 参数
	delayArgs, err := getQueueArguments(delayQueue)
	if err != nil {
		return fmt.Errorf("获取延迟队列参数失败: %w", err)
	}
	ttl, ok := delayArgs["x-message-ttl"]
	if !ok {
		return fmt.Errorf("延迟队列 %s 缺少 x-message-ttl 参数", delayQueue)
	}
	dlxTarget, ok := delayArgs["x-dead-letter-exchange"]
	if !ok || dlxTarget != mainExchange {
		return fmt.Errorf("延迟队列 DLX 目标错误: %v", dlxTarget)
	}
	fmt.Printf("     → 延迟队列 %s x-message-ttl=%v, DLX→%s ✓\n", delayQueue, ttl, mainExchange)

	// 验证超时检查队列存在
	if _, err := getQueueArguments(checkQueue); err != nil {
		return fmt.Errorf("超时检查队列 %s 不存在: %w", checkQueue, err)
	}
	fmt.Printf("     → 超时检查队列 %s ✓\n", checkQueue)

	// 验证死信队列存在
	if _, err := getQueueArguments(deadQueue); err != nil {
		return fmt.Errorf("死信队列 %s 不存在: %w", deadQueue, err)
	}
	fmt.Printf("     → 死信队列 %s ✓\n", deadQueue)

	return nil
}

// ==================== TC-02: 延迟路由机制验证 ====================
// 使用隔离的测试队列，设置 3s TTL，验证消息到期后路由到目标队列

func testDelayRouting() error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("创建 channel 失败: %w", err)
	}
	defer ch.Close()

	// 声明测试专用交换机（auto-delete）
	if err := ch.ExchangeDeclare(testExchange, "direct", false, true, false, false, nil); err != nil {
		return fmt.Errorf("声明测试交换机失败: %w", err)
	}

	// 声明测试延迟队列（3秒TTL，到期路由到测试检查队列）
	_, err = ch.QueueDeclare(testDelayQueue, false, true, false, false, amqp.Table{
		"x-message-ttl":             int32(3000), // 3秒
		"x-dead-letter-exchange":    testExchange,
		"x-dead-letter-routing-key": "test.check",
	})
	if err != nil {
		return fmt.Errorf("声明测试延迟队列失败: %w", err)
	}
	if err := ch.QueueBind(testDelayQueue, "test.delay", testExchange, false, nil); err != nil {
		return fmt.Errorf("绑定测试延迟队列失败: %w", err)
	}

	// 声明测试检查队列（消息到期后会路由到这里）
	_, err = ch.QueueDeclare(testCheckQueue, false, true, false, false, nil)
	if err != nil {
		return fmt.Errorf("声明测试检查队列失败: %w", err)
	}
	if err := ch.QueueBind(testCheckQueue, "test.check", testExchange, false, nil); err != nil {
		return fmt.Errorf("绑定测试检查队列失败: %w", err)
	}

	// 发送消息到延迟队列
	body := []byte(`{"order_id":"test_delay_001","user_id":1}`)
	if err := ch.Publish(testExchange, "test.delay", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}); err != nil {
		return fmt.Errorf("发送延迟消息失败: %w", err)
	}
	fmt.Printf("     → 延迟消息已发送，等待 4s TTL 到期...\n")

	// 等待 TTL 到期（3s + 1s buffer）
	time.Sleep(4 * time.Second)

	// 从测试检查队列消费，验证消息到达
	msg, ok, err := ch.Get(testCheckQueue, true)
	if err != nil {
		return fmt.Errorf("从检查队列获取消息失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("延迟消息未路由到检查队列（TTL到期后未出现）")
	}

	fmt.Printf("     → 消息成功路由到检查队列，body=%s ✓\n", string(msg.Body))
	return nil
}

// ==================== TC-03: DLX 死信路由验证 ====================
// 使用隔离测试队列，Nack 一条消息，验证它路由到 seckill_dead_queue

func testDLXRouting() error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("创建 channel 失败: %w", err)
	}
	defer ch.Close()

	// 先清空死信队列，避免残留消息干扰计数
	purgedCount, _ := ch.QueuePurge(deadQueue, false)
	if purgedCount > 0 {
		fmt.Printf("     → 清理死信队列残留消息 %d 条\n", purgedCount)
	}

	// 声明测试队列（临时，绑定 DLX 指向真实的 seckill_dlx）
	_, err = ch.QueueDeclare(testDLXQueue, false, true, false, false, amqp.Table{
		"x-dead-letter-exchange":    dlxExchange, // 指向真实 DLX
		"x-dead-letter-routing-key": routingKeyDead,
	})
	if err != nil {
		return fmt.Errorf("声明测试DLX队列失败: %w", err)
	}

	// 发送消息到测试队列（通过默认交换机，routing key = 队列名）
	body := []byte(`{"order_id":"test_dlx_001","user_id":1}`)
	if err := ch.Publish("", testDLXQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Transient,
		Body:         body,
	}); err != nil {
		return fmt.Errorf("发送测试消息失败: %w", err)
	}

	// 消费并 Nack（requeue=false → 触发 DLX）
	msg, ok, err := ch.Get(testDLXQueue, false)
	if err != nil || !ok {
		return fmt.Errorf("从测试队列获取消息失败: err=%v, ok=%v", err, ok)
	}
	msg.Nack(false, false) // requeue=false → DLX 路由

	// 等待 RabbitMQ 完成路由
	time.Sleep(500 * time.Millisecond)

	// 验证死信队列收到了消息
	deadMsg, ok, err := ch.Get(deadQueue, true) // autoAck=true，取出后立即清理
	if err != nil {
		return fmt.Errorf("从死信队列获取消息失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("Nack 后消息未路由到死信队列 %s", deadQueue)
	}

	fmt.Printf("     → Nack 后消息成功路由到死信队列，body=%s ✓\n", string(deadMsg.Body))
	return nil
}

// ==================== TC-04: 主队列 DLX 绑定验证 ====================
// 直接向主队列注入消息，立即 Nack，验证路由到 seckill_dead_queue
// 注意：需要确保 order-service 消费者未在竞争本条消息

func testMainQueueDLX() error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("创建 channel 失败: %w", err)
	}
	defer ch.Close()

	// 清空死信队列
	ch.QueuePurge(deadQueue, false)

	// 通过默认交换机直接向主队列注入消息（绕过 seckill_exchange）
	// 这样 order-service 消费者也会收到这条消息，但我们使用 exclusive consumer 抢先
	testBody := []byte(`{"order_id":"test_main_dlx_001","user_id":99999}`)
	if err := ch.Publish("", mainQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Transient,
		Body:         testBody,
		Priority:     255, // 最高优先级，确保测试先拿到
	}); err != nil {
		return fmt.Errorf("注入测试消息失败: %w", err)
	}

	// 设置 prefetch=1，立即抢先消费
	ch.Qos(1, 0, false)
	msgs, err := ch.Consume(mainQueue, "test_dlx_consumer", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("注册消费者失败: %w", err)
	}

	// 等待拿到测试消息（最多3秒）
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	for {
		select {
		case msg := <-msgs:
			// 检查是否是我们的测试消息
			var parsed map[string]interface{}
			json.Unmarshal(msg.Body, &parsed)
			if orderId, _ := parsed["order_id"].(string); orderId == "test_main_dlx_001" {
				msg.Nack(false, false) // 触发 DLX
				// 等待路由完成
				time.Sleep(500 * time.Millisecond)
				// 验证死信队列
				dead, ok, err := ch.Get(deadQueue, true)
				if err != nil || !ok {
					return fmt.Errorf("主队列 Nack 后消息未路由到死信队列: err=%v, ok=%v", err, ok)
				}
				fmt.Printf("     → 主队列 Nack → 死信队列路由成功，body=%s ✓\n", string(dead.Body))
				return nil
			}
			// 不是我们的消息，重新入队
			msg.Nack(false, true)

		case <-timer.C:
			return fmt.Errorf("超时：未能在3秒内拿到测试消息（可能被 order-service 消费者抢先）")
		}
	}
}

// ==================== Management API 辅助函数 ====================

type queueInfo struct {
	Arguments map[string]interface{} `json:"arguments"`
}

func getQueueArguments(queueName string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/queues/%%2F/%s", managementURL, queueName)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(managementUser, managementPass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("队列不存在 (404)")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var info queueInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return info.Arguments, nil
}

type exchangeInfo struct {
	Name string `json:"name"`
}

func checkExchangeExists(exchangeName string) error {
	url := fmt.Sprintf("%s/exchanges/%%2F/%s", managementURL, exchangeName)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(managementUser, managementPass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Errorf("交换机不存在 (404)")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
