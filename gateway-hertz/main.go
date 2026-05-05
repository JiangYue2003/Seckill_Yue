package main

import (
	"context"
	"flag"
	"fmt"

	"seckill-mall/gateway-hertz/internal/client"
	"seckill-mall/gateway-hertz/internal/config"
	"seckill-mall/gateway-hertz/internal/handler"
	"seckill-mall/gateway-hertz/internal/middleware"
	"seckill-mall/gateway-hertz/internal/svc"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/hertz-contrib/monitor-prometheus"
	"github.com/zeromicro/go-zero/core/conf"
)

var configFile = flag.String("f", "etc/gateway.yaml", "the config file")
var port = flag.Int("port", 0, "override listen port")
var metricsPort = flag.Int("metrics-port", 0, "override prometheus metrics port")

const defaultMetricsPort = 9181

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	if *port > 0 {
		c.Port = *port
	}
	mPort := defaultMetricsPort
	if c.MetricsPort > 0 {
		mPort = c.MetricsPort
	}
	if *metricsPort > 0 {
		mPort = *metricsPort
	}

	// 初始化 gRPC 客户端
	clients, err := client.NewClientManager(c)
	if err != nil {
		panic(fmt.Sprintf("初始化 gRPC 客户端失败: %v", err))
	}

	// 初始化服务上下文（含 Redis）
	svcCtx := svc.NewServiceContext(c, clients)

	// 初始化 Hertz 服务器
	h := server.Default(
		server.WithHostPorts(fmt.Sprintf("%s:%d", c.Host, c.Port)),
		server.WithTracer(prometheus.NewServerTracer(
			fmt.Sprintf(":%d", mPort), "/metrics",
		)),
	)

	// 全局中间件
	h.Use(middleware.CORS())
	h.Use(middleware.RequestLogger())

	// 健康检查（无需认证）
	h.GET("/health", handler.HealthHandler)

	// 用户公开路由
	userHandler := handler.NewUserHandler(clients.UserService)
	userGroup := h.Group("/api/v1/user")
	{
		userGroup.POST("/register", userHandler.Register)
		userGroup.POST("/login", userHandler.Login)
		userGroup.POST("/refresh", userHandler.RefreshToken)
	}

	// JWT 认证中间件
	jwtAuth := middleware.JWTAuth(c.JWT.AccessSecret, svcCtx.Redis)

	// 受保护路由
	api := h.Group("/api/v1", jwtAuth)
	{
		// 用户信息
		api.GET("/user/info", userHandler.GetUserInfo)
		api.PUT("/user/info", userHandler.UpdateUserInfo)
		api.POST("/user/password", userHandler.ChangePassword)

		// 商品
		productHandler := handler.NewProductHandler(clients.ProductService)
		api.GET("/product/:id", productHandler.GetProduct)
		api.GET("/products", productHandler.ListProducts)
		api.GET("/seckill/products", productHandler.ListSeckillProducts)

		// 秒杀（可选限流）
		seckillHandler := handler.NewSeckillHandler(clients.SeckillService)
		seckillGroup := api.Group("/seckill")
		if c.RateLimit.Enabled {
			strategy, strategyErr := middleware.NewRateLimitStrategy(c.RedisHost, middleware.RateLimitConfig{
				Strategy: c.RateLimit.Strategy,
				QPS:      c.RateLimit.QPS,
				Capacity: c.RateLimit.Capacity,
			})
			if strategyErr != nil {
				panic(fmt.Sprintf("初始化限流策略失败: %v", strategyErr))
			}
			hlog.Infof("Seckill rate limit enabled: strategy=%s qps=%d capacity=%d",
				c.RateLimit.Strategy, c.RateLimit.QPS, c.RateLimit.Capacity)
			seckillGroup.Use(middleware.RateLimitMiddleware(strategy))
		} else {
			hlog.Info("Seckill rate limit disabled by config")
		}
		seckillGroup.POST("", seckillHandler.Seckill)
		seckillGroup.GET("/status", seckillHandler.GetSeckillStatus)
		seckillGroup.GET("/result", seckillHandler.GetSeckillResult)

		// 订单
		orderHandler := handler.NewOrderHandler(clients.OrderService)
		api.POST("/order", orderHandler.CreateNormalOrder)
		api.GET("/order/:orderId", orderHandler.GetOrder)
		api.GET("/orders", orderHandler.ListOrders)
		api.POST("/order/:orderId/cancel", orderHandler.CancelOrder)
		api.POST("/order/pay", orderHandler.PayOrder)
		api.POST("/order/:orderId/refund", orderHandler.RefundOrder)
	}

	hlog.Infof("Starting gateway-hertz at %s:%d (metrics=:%d)", c.Host, c.Port, mPort)

	// Spin 内置优雅关闭
	h.Spin()

	// 关闭 Redis 连接
	_ = svcCtx.Redis.Shutdown(context.Background())
}

// ensure app import is used (Hertz handler signature requires it)
var _ = (*app.RequestContext)(nil)
