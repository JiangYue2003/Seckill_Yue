package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"seckill-mall/gateway/internal/client"
	"seckill-mall/gateway/internal/config"
	"seckill-mall/gateway/internal/handler"
	"seckill-mall/gateway/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/conf"
	goprometheus "github.com/zeromicro/go-zero/core/prometheus"
	"github.com/zeromicro/go-zero/core/trace"
)

var configFile = flag.String("f", "etc/gateway.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 初始化链路追踪（上报到 Jaeger）
	if !c.Telemetry.Disabled {
		trace.StartAgent(c.Telemetry)
		defer trace.StopAgent()
	}

	// 初始化 Prometheus 指标暴露（/metrics 端口 9180）
	goprometheus.StartAgent(goprometheus.Config{
		Host: "0.0.0.0",
		Port: 9180,
		Path: "/metrics",
	})

	// 初始化 gRPC 客户端
	clients, err := client.NewClientManager(c)
	if err != nil {
		panic(fmt.Sprintf("初始化 gRPC 客户端失败: %v", err))
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery()) // 捕获 panic，避免进程崩溃
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORS())

	// 设置最大并发连接数（避免资源耗尽）
	server := &http.Server{
		Addr:           fmt.Sprintf("%s:%d", c.Host, c.Port),
		Handler:        r,
		MaxHeaderBytes: 1 << 20, // 1MB
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    60 * time.Second,
	}

	r.GET("/health", handler.HealthHandler)

	// 用户相关路由（无需登录）
	userGroup := r.Group("/api/v1/user")
	{
		userHandler := handler.NewUserHandler(clients.UserService)
		userGroup.POST("/register", userHandler.Register)
		userGroup.POST("/login", userHandler.Login)
		userGroup.POST("/refresh", userHandler.RefreshToken)
	}

	// 需要登录的路由
	authGroup := r.Group("/api/v1")
	authGroup.Use(middleware.JWTAuth(c.JWT.AccessSecret, c.RedisHost))
	{
		userHandler := handler.NewUserHandler(clients.UserService)
		authGroup.GET("/user/info", userHandler.GetUserInfo)
		authGroup.PUT("/user/info", userHandler.UpdateUserInfo)
		authGroup.POST("/user/password", userHandler.ChangePassword)

		productHandler := handler.NewProductHandler(clients.ProductService)
		authGroup.GET("/product/:id", productHandler.GetProduct)
		authGroup.GET("/products", productHandler.ListProducts)
		authGroup.GET("/seckill/products", productHandler.ListSeckillProducts)

		// 秒杀路由：工厂模式限流（策略可配置，yaml 中切换）
		seckillHandler := handler.NewSeckillHandler(clients.SeckillService)
		seckillGroup := authGroup.Group("/seckill")

		seckillStrategy, err := middleware.NewRateLimitStrategy(c.RedisHost, middleware.RateLimitConfig{
			Strategy: c.RateLimit.Strategy,
			QPS:      c.RateLimit.QPS,
			Capacity: c.RateLimit.Capacity,
		})
		if err != nil {
			panic(fmt.Sprintf("初始化限流策略失败: %v", err))
		}
		fmt.Printf("Seckill rate limit enabled: strategy=%s qps=%d capacity=%d\n", c.RateLimit.Strategy, c.RateLimit.QPS, c.RateLimit.Capacity)
		seckillGroup.Use(middleware.RateLimitMiddleware(seckillStrategy))

		seckillGroup.POST("", seckillHandler.Seckill)
		seckillGroup.GET("/status", seckillHandler.GetSeckillStatus)
		seckillGroup.GET("/result", seckillHandler.GetSeckillResult)

		orderHandler := handler.NewOrderHandler(clients.OrderService)
		authGroup.POST("/order", orderHandler.CreateNormalOrder)
		authGroup.GET("/order/:orderId", orderHandler.GetOrder)
		authGroup.GET("/orders", orderHandler.ListOrders)
		authGroup.POST("/order/:orderId/cancel", orderHandler.CancelOrder)
		authGroup.POST("/order/pay", orderHandler.PayOrder)
		authGroup.POST("/order/:orderId/refund", orderHandler.RefundOrder)
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	fmt.Printf("Starting gateway server at %s...\n", addr)
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}
