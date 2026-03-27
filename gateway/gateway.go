package main

import (
	"flag"
	"fmt"
	"net/http"

	"seckill-mall/gateway/internal/client"
	"seckill-mall/gateway/internal/config"
	"seckill-mall/gateway/internal/handler"
	"seckill-mall/gateway/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/conf"
)

var configFile = flag.String("f", "etc/gateway.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	// 初始化 gRPC 客户端
	clients, err := client.NewClientManager(c)
	if err != nil {
		panic(fmt.Sprintf("初始化 gRPC 客户端失败: %v", err))
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORS())

	r.GET("/health", handler.HealthHandler)

	// 用户相关路由（无需登录）
	userGroup := r.Group("/api/v1/user")
	{
		userHandler := handler.NewUserHandler(clients.UserService)
		userGroup.POST("/register", userHandler.Register)
		userGroup.POST("/login", userHandler.Login)
	}

	// 需要登录的路由
	authGroup := r.Group("/api/v1")
	authGroup.Use(middleware.JWTAuth(c.JWT.AccessSecret))
	{
		userHandler := handler.NewUserHandler(clients.UserService)
		authGroup.GET("/user/info", userHandler.GetUserInfo)
		authGroup.PUT("/user/info", userHandler.UpdateUserInfo)
		authGroup.POST("/user/password", userHandler.ChangePassword)

		productHandler := handler.NewProductHandler(clients.ProductService)
		authGroup.GET("/product/:id", productHandler.GetProduct)
		authGroup.GET("/products", productHandler.ListProducts)
		authGroup.GET("/seckill/products", productHandler.ListSeckillProducts)

		seckillHandler := handler.NewSeckillHandler(clients.SeckillService)
		authGroup.POST("/seckill", seckillHandler.Seckill)
		authGroup.GET("/seckill/status", seckillHandler.GetSeckillStatus)
		authGroup.GET("/seckill/result", seckillHandler.GetSeckillResult)

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
	if err := http.ListenAndServe(addr, r); err != nil {
		panic(err)
	}
}
