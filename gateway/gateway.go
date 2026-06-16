package main

import (
	"flag"
	"fmt"
	"net/http"
	"time"

	"seckill-mall/common/logutil"
	"seckill-mall/gateway/internal/client"
	"seckill-mall/gateway/internal/config"
	"seckill-mall/gateway/internal/handler"
	"seckill-mall/gateway/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	goprometheus "github.com/zeromicro/go-zero/core/prometheus"
	"github.com/zeromicro/go-zero/core/trace"
)

var configFile = flag.String("f", "etc/gateway.yaml", "the config file")
var port = flag.Int("port", 0, "override gateway http listen port, e.g. --port=18888")
var metricsPort = flag.Int("metrics-port", 0, "override prometheus metrics port, e.g. --metrics-port=19180")

const defaultMetricsPort = 9180

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	metricsListenPort := overridePorts(&c)
	logx.MustSetup(c.Log)
	defer logx.Close()
	logutil.SetupInstanceFields(c.Log.ServiceName, *port)

	// 初始化链路追踪（上报到 Jaeger）
	if !c.Telemetry.Disabled {
		trace.StartAgent(c.Telemetry)
		defer trace.StopAgent()
	}

	// 初始化 Prometheus 指标暴露（默认 /metrics 端口 9180，可被 --metrics-port 覆盖）
	goprometheus.StartAgent(goprometheus.Config{
		Host: "0.0.0.0",
		Port: metricsListenPort,
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

		if c.RateLimit.Enabled {
			seckillStrategy, err := middleware.NewRateLimitStrategy(c.RedisHost, middleware.RateLimitConfig{
				Strategy: c.RateLimit.Strategy,
				QPS:      c.RateLimit.QPS,
				Capacity: c.RateLimit.Capacity,
			})
			if err != nil {
				panic(fmt.Sprintf("初始化限流策略失败: %v", err))
			}
			logx.Infof("Seckill rate limit enabled: strategy=%s qps=%d capacity=%d", c.RateLimit.Strategy, c.RateLimit.QPS, c.RateLimit.Capacity)
			seckillGroup.Use(middleware.RateLimitMiddleware(seckillStrategy))
		} else {
			logx.Info("Seckill rate limit disabled by config")
		}

		seckillGroup.POST("", seckillHandler.Seckill)
		seckillGroup.GET("/status", seckillHandler.GetSeckillStatus)
		seckillGroup.GET("/result", seckillHandler.GetSeckillResult)

		orderHandler := handler.NewOrderHandler(clients.OrderService)
		r.POST("/api/v1/payment/callback/mock", orderHandler.HandleMockPaymentCallback)
		authGroup.POST("/payment", orderHandler.CreatePayment)
		authGroup.GET("/payment", orderHandler.GetPayment)
		authGroup.POST("/order", orderHandler.CreateNormalOrder)
		authGroup.GET("/order/:orderId", orderHandler.GetOrder)
		authGroup.GET("/orders", orderHandler.ListOrders)
		authGroup.POST("/order/:orderId/cancel", orderHandler.CancelOrder)
		authGroup.POST("/order/pay", orderHandler.PayOrder)
		authGroup.POST("/order/:orderId/refund", orderHandler.RefundOrder)
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	logx.Infof("Starting gateway server at %s (metrics=0.0.0.0:%d)...", addr, metricsListenPort)
	if err := server.ListenAndServe(); err != nil {
		logx.Errorf("gateway server exited: %v", err)
		panic(err)
	}
}

func overridePorts(c *config.Config) int {
	if *port > 0 {
		c.Port = *port
	}

	metricsListenPort := defaultMetricsPort
	if *metricsPort > 0 {
		metricsListenPort = *metricsPort
	}

	return metricsListenPort
}
