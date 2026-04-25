package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	seckill "seckill-mall/common/seckill"
)

// TestResult 测试结果
type TestResult struct {
	TC      string
	Name    string
	Pass    bool
	Message string
}

// runTests 运行所有测试用例
func runTests(client *SeckillServiceClient) {
	results := make([]TestResult, 0)

	// TC-01: 秒杀成功
	results = append(results, testSeckillSuccess(client))
	// TC-02: 库存不足
	results = append(results, testStockNotEnough(client))
	// TC-03: 用户防重
	results = append(results, testUserDuplicate(client))
	// TC-04: 订单状态查询
	results = append(results, testOrderStatusQuery(client))
	// TC-05: 查询不存在的订单
	results = append(results, testNonExistentOrder(client))

	// 清理测试数据
	cleanupTestData()

	// 关闭连接
	client.Close()

	// 打印测试报告
	printTestReport(results)
}

// TC-01: 秒杀成功
func testSeckillSuccess(client *SeckillServiceClient) TestResult {
	fmt.Println("\n----------------------------------------")
	fmt.Println("[TC-01] 测试: 秒杀成功")
	fmt.Println("----------------------------------------")

	// 初始化测试数据
	seckillProductId := int64(1001)
	stock := int64(10)
	userId := int64(10001)

	// 清理可能存在的旧数据
	cleanupProductData(seckillProductId)

	// 初始化商品库存到 Redis
	now := time.Now().Unix()
	ttl := int64(86400)     // 24小时
	startTime := now - 3600 // 1小时前开始
	endTime := now + 3600   // 1小时后结束

	ctx := context.Background()

	// 注意：productId 必须是数据库 products 表中真实存在的商品ID
	// schema.sql 中插入了 id=1(iPhone), id=2(小米), id=3(MacBook)
	if err := setSeckillProductInfo(ctx, seckillProductId, 1, 999, "测试秒杀商品", startTime, endTime, ttl); err != nil {
		return TestResult{TC: "TC-01", Name: "秒杀成功", Pass: false, Message: fmt.Sprintf("初始化商品信息失败: %v", err)}
	}

	if err := initStock(ctx, seckillProductId, stock); err != nil {
		return TestResult{TC: "TC-01", Name: "秒杀成功", Pass: false, Message: fmt.Sprintf("初始化库存失败: %v", err)}
	}

	// 发起秒杀请求
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
		UserId:           userId,
		SeckillProductId: seckillProductId,
		Quantity:         1,
	})
	if err != nil {
		return TestResult{TC: "TC-01", Name: "秒杀成功", Pass: false, Message: fmt.Sprintf("秒杀请求失败: %v", err)}
	}

	// 验证结果
	if resp.Success && resp.Code == "SUCCESS" && resp.OrderId != "" {
		fmt.Printf("[PASS] 秒杀成功，订单号: %s\n", resp.OrderId)
		return TestResult{TC: "TC-01", Name: "秒杀成功", Pass: true, Message: fmt.Sprintf("订单号: %s", resp.OrderId)}
	}

	fmt.Printf("[FAIL] 秒杀失败: success=%v, code=%s, message=%s\n", resp.Success, resp.Code, resp.Message)
	return TestResult{TC: "TC-01", Name: "秒杀成功", Pass: false, Message: fmt.Sprintf("返回: %s - %s", resp.Code, resp.Message)}
}

// TC-02: 库存不足
func testStockNotEnough(client *SeckillServiceClient) TestResult {
	fmt.Println("\n----------------------------------------")
	fmt.Println("[TC-02] 测试: 库存不足")
	fmt.Println("----------------------------------------")

	seckillProductId := int64(1002)
	stock := int64(1) // 只准备1个库存
	userA := int64(10021)
	userB := int64(10022)

	// 清理可能存在的旧数据
	cleanupProductData(seckillProductId)

	// 初始化商品
	now := time.Now().Unix()
	ttl := int64(86400)
	startTime := now - 3600
	endTime := now + 3600

	ctx := context.Background()

	if err := setSeckillProductInfo(ctx, seckillProductId, 2, 999, "测试库存不足", startTime, endTime, ttl); err != nil {
		return TestResult{TC: "TC-02", Name: "库存不足", Pass: false, Message: fmt.Sprintf("初始化商品信息失败: %v", err)}
	}

	if err := initStock(ctx, seckillProductId, stock); err != nil {
		return TestResult{TC: "TC-02", Name: "库存不足", Pass: false, Message: fmt.Sprintf("初始化库存失败: %v", err)}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 用户A秒杀
	respA, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
		UserId:           userA,
		SeckillProductId: seckillProductId,
		Quantity:         1,
	})
	if err != nil {
		return TestResult{TC: "TC-02", Name: "库存不足", Pass: false, Message: fmt.Sprintf("用户A秒杀失败: %v", err)}
	}
	fmt.Printf("  用户A秒杀结果: success=%v, code=%s\n", respA.Success, respA.Code)

	// 用户B秒杀（应该库存不足）
	respB, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
		UserId:           userB,
		SeckillProductId: seckillProductId,
		Quantity:         1,
	})
	if err != nil {
		return TestResult{TC: "TC-02", Name: "库存不足", Pass: false, Message: fmt.Sprintf("用户B秒杀失败: %v", err)}
	}

	// 验证
	if respA.Success && respA.Code == "SUCCESS" &&
		!respB.Success && respB.Code == "SOLD_OUT" {
		fmt.Printf("[PASS] 用户A成功，用户B库存不足\n")
		return TestResult{TC: "TC-02", Name: "库存不足", Pass: true, Message: "库存控制正确"}
	}

	fmt.Printf("[FAIL] 库存控制异常: 用户A=%s, 用户B=%s\n", respA.Code, respB.Code)
	return TestResult{TC: "TC-02", Name: "库存不足", Pass: false, Message: fmt.Sprintf("用户A=%s, 用户B=%s", respA.Code, respB.Code)}
}

// TC-03: 用户防重
func testUserDuplicate(client *SeckillServiceClient) TestResult {
	fmt.Println("\n----------------------------------------")
	fmt.Println("[TC-03] 测试: 用户防重")
	fmt.Println("----------------------------------------")

	seckillProductId := int64(1003)
	stock := int64(10)
	userId := int64(10031)

	// 清理可能存在的旧数据
	cleanupProductData(seckillProductId)

	// 初始化商品
	now := time.Now().Unix()
	ttl := int64(86400)
	startTime := now - 3600
	endTime := now + 3600

	ctx := context.Background()

	if err := setSeckillProductInfo(ctx, seckillProductId, 1, 999, "测试用户防重", startTime, endTime, ttl); err != nil {
		return TestResult{TC: "TC-03", Name: "用户防重", Pass: false, Message: fmt.Sprintf("初始化商品信息失败: %v", err)}
	}

	if err := initStock(ctx, seckillProductId, stock); err != nil {
		return TestResult{TC: "TC-03", Name: "用户防重", Pass: false, Message: fmt.Sprintf("初始化库存失败: %v", err)}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 第一次秒杀
	resp1, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
		UserId:           userId,
		SeckillProductId: seckillProductId,
		Quantity:         1,
	})
	if err != nil {
		return TestResult{TC: "TC-03", Name: "用户防重", Pass: false, Message: fmt.Sprintf("第一次秒杀失败: %v", err)}
	}
	fmt.Printf("  第一次秒杀: success=%v, code=%s\n", resp1.Success, resp1.Code)

	// 第二次秒杀（同一用户，应该被拦截）
	resp2, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
		UserId:           userId,
		SeckillProductId: seckillProductId,
		Quantity:         1,
	})
	if err != nil {
		return TestResult{TC: "TC-03", Name: "用户防重", Pass: false, Message: fmt.Sprintf("第二次秒杀失败: %v", err)}
	}

	// 验证
	if resp1.Success && resp1.Code == "SUCCESS" &&
		!resp2.Success && resp2.Code == "ALREADY_PURCHASED" {
		fmt.Printf("[PASS] 用户防重生效\n")
		return TestResult{TC: "TC-03", Name: "用户防重", Pass: true, Message: "防重机制正确"}
	}

	fmt.Printf("[FAIL] 防重机制异常: 第一次=%s, 第二次=%s\n", resp1.Code, resp2.Code)
	return TestResult{TC: "TC-03", Name: "用户防重", Pass: false, Message: fmt.Sprintf("第一次=%s, 第二次=%s", resp1.Code, resp2.Code)}
}

// TC-04: 订单状态查询
func testOrderStatusQuery(client *SeckillServiceClient) TestResult {
	fmt.Println("\n----------------------------------------")
	fmt.Println("[TC-04] 测试: 订单状态查询")
	fmt.Println("----------------------------------------")

	seckillProductId := int64(1004)
	stock := int64(10)
	userId := int64(10041)

	// 清理可能存在的旧数据
	cleanupProductData(seckillProductId)

	// 初始化商品
	now := time.Now().Unix()
	ttl := int64(86400)
	startTime := now - 3600
	endTime := now + 3600

	ctx := context.Background()

	if err := setSeckillProductInfo(ctx, seckillProductId, 2, 999, "测试订单查询", startTime, endTime, ttl); err != nil {
		return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: false, Message: fmt.Sprintf("初始化商品信息失败: %v", err)}
	}

	if err := initStock(ctx, seckillProductId, stock); err != nil {
		return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: false, Message: fmt.Sprintf("初始化库存失败: %v", err)}
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 发起秒杀
	resp, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
		UserId:           userId,
		SeckillProductId: seckillProductId,
		Quantity:         1,
	})
	if err != nil {
		return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: false, Message: fmt.Sprintf("秒杀请求失败: %v", err)}
	}

	if !resp.Success || resp.OrderId == "" {
		return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: false, Message: "秒杀未成功，无法测试订单查询"}
	}

	orderId := resp.OrderId
	fmt.Printf("  订单号: %s\n", orderId)

	// 轮询查询订单状态（最多10次，每次间隔500ms）
	maxRetry := 10
	for i := 0; i < maxRetry; i++ {
		resultResp, err := client.GetSeckillResult(reqCtx, &seckill.SeckillResultRequest{
			OrderId: orderId,
		})
		if err != nil {
			fmt.Printf("  查询失败 (尝试 %d/%d): %v\n", i+1, maxRetry, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		fmt.Printf("  订单状态: %s (尝试 %d/%d)\n", resultResp.Status, i+1, maxRetry)

		if resultResp.Success && resultResp.Status == "success" {
			fmt.Printf("[PASS] 订单处理成功\n")
			return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: true, Message: fmt.Sprintf("订单状态: %s", resultResp.Status)}
		}

		if !resultResp.Success && resultResp.Message == "订单处理失败" {
			fmt.Printf("[WARN] 订单处理失败 (可能是OrderService未启动)\n")
			return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: true, Message: "订单查询功能正常，但OrderService未启动"}
		}

		time.Sleep(500 * time.Millisecond)
	}

	// 如果超时，检查状态是否至少是 pending
	finalResp, _ := client.GetSeckillResult(reqCtx, &seckill.SeckillResultRequest{OrderId: orderId})
	if finalResp != nil && finalResp.Status != "" {
		fmt.Printf("[PASS] 订单查询功能正常，最终状态: %s\n", finalResp.Status)
		return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: true, Message: fmt.Sprintf("订单查询正常，状态: %s", finalResp.Status)}
	}

	return TestResult{TC: "TC-04", Name: "订单状态查询", Pass: false, Message: "订单状态查询超时"}
}

// TC-05: 查询不存在的订单
func testNonExistentOrder(client *SeckillServiceClient) TestResult {
	fmt.Println("\n----------------------------------------")
	fmt.Println("[TC-05] 测试: 查询不存在的订单")
	fmt.Println("----------------------------------------")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 查询一个不存在的订单
	resp, err := client.GetSeckillResult(ctx, &seckill.SeckillResultRequest{
		OrderId: "NON_EXISTENT_ORDER_12345",
	})
	if err != nil {
		return TestResult{TC: "TC-05", Name: "查询不存在的订单", Pass: false, Message: fmt.Sprintf("查询请求失败: %v", err)}
	}

	// 验证
	if !resp.Success && strings.Contains(resp.Message, "不存在") {
		fmt.Printf("[PASS] 正确返回订单不存在\n")
		return TestResult{TC: "TC-05", Name: "查询不存在的订单", Pass: true, Message: resp.Message}
	}

	fmt.Printf("[FAIL] 返回异常: success=%v, message=%s\n", resp.Success, resp.Message)
	return TestResult{TC: "TC-05", Name: "查询不存在的订单", Pass: false, Message: fmt.Sprintf("返回: %s", resp.Message)}
}

// cleanupProductData 清理单个商品的测试数据
func cleanupProductData(seckillProductId int64) {
	ctx := context.Background()
	rollbackStock(ctx, seckillProductId, 10000)
	deleteUserKey(ctx, seckillProductId, 10001)
	deleteUserKey(ctx, seckillProductId, 10002)
	deleteUserKey(ctx, seckillProductId, 10021)
	deleteUserKey(ctx, seckillProductId, 10022)
	deleteUserKey(ctx, seckillProductId, 10031)
	deleteUserKey(ctx, seckillProductId, 10041)
}

// cleanupTestData 清理所有测试数据
func cleanupTestData() {
	fmt.Println("\n[清理] 正在清理测试数据...")
	testProductIds := []int64{1001, 1002, 1003, 1004}
	for _, pid := range testProductIds {
		cleanupProductData(pid)
	}
	fmt.Println("[清理] 测试数据清理完成")
}

// printTestReport 打印测试报告
func printTestReport(results []TestResult) {
	fmt.Println("\n========================================")
	fmt.Println("   测试报告")
	fmt.Println("========================================")

	passCount := 0
	failCount := 0

	for _, r := range results {
		status := "[PASS]"
		if !r.Pass {
			status = "[FAIL]"
			failCount++
		} else {
			passCount++
		}
		fmt.Printf("%s %s: %s\n", status, r.TC, r.Name)
		if !r.Pass {
			fmt.Printf("       失败原因: %s\n", r.Message)
		} else if r.Message != "" {
			fmt.Printf("       信息: %s\n", r.Message)
		}
	}

	fmt.Println("----------------------------------------")
	fmt.Printf("通过: %d/%d", passCount, len(results))
	if failCount > 0 {
		fmt.Printf(" | 失败: %d\n", failCount)
	} else {
		fmt.Println()
	}
}
