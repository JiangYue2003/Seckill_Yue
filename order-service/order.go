package main

import (
	"flag"
	"fmt"

	"seckill-mall/order-service/internal/config"
	"seckill-mall/order-service/internal/server"
	"seckill-mall/order-service/internal/svc"
	"seckill-mall/order-service/order"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/order.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	// 启动 RabbitMQ Consumer
	if ctx.Consumer != nil {
		if err := ctx.Consumer.Start(); err != nil {
			fmt.Printf("Warning: RabbitMQ consumer failed to start: %v\n", err)
		}
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		order.RegisterOrderServiceServer(grpcServer, server.NewOrderServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer func() {
		s.Stop()
		if ctx.Consumer != nil {
			err := ctx.Consumer.Stop()
			if err != nil {
				return
			}
		}
	}()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start()
}
