package main

import (
	"flag"
	"fmt"
	"net"
	"strconv"

	"seckill-mall/common/logutil"
	"seckill-mall/common/order"
	"seckill-mall/order-service/internal/config"
	"seckill-mall/order-service/internal/server"
	"seckill-mall/order-service/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/order.yaml", "the config file")
var port = flag.Int("port", 0, "override rpc listen port, e.g. --port=19084")
var metricsPort = flag.Int("metrics-port", 0, "override prometheus port, e.g. --metrics-port=19184")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	overridePorts(&c)
	if err := logutil.ResetLogsIfEnabled(c.Mode, c.Dev.ResetLogsOnStart, c.Log); err != nil {
		panic(fmt.Sprintf("failed to reset logs: %v", err))
	}
	logx.MustSetup(c.Log)
	defer logx.Close()
	ctx := svc.NewServiceContext(c)

	// 启动主处理队列消费者
	if ctx.Consumer != nil {
		if err := ctx.Consumer.Start(); err != nil {
			logx.Errorf("RabbitMQ consumer failed to start: %v", err)
		}
	}
	// 启动超时检查队列消费者
	if ctx.CheckConsumer != nil {
		if err := ctx.CheckConsumer.Start(); err != nil {
			logx.Errorf("RabbitMQ check consumer failed to start: %v", err)
		}
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		order.RegisterOrderServiceServer(grpcServer, server.NewOrderServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer func() {
		// 1. 停止消费新消息
		if ctx.Consumer != nil {
			_ = ctx.Consumer.Stop()
		}
		if ctx.CheckConsumer != nil {
			_ = ctx.CheckConsumer.Stop()
		}

		// 2. 刷完 BatchWriter 缓冲区
		if ctx.BatchWriter != nil {
			ctx.BatchWriter.Shutdown()
		}

		// 3. 停止 gRPC 服务
		s.Stop()
	}()

	logx.Infof("Starting rpc server at %s...", c.ListenOn)
	s.Start()
}

func overridePorts(c *config.Config) {
	if *port > 0 {
		listenOn, err := replacePort(c.ListenOn, *port)
		if err != nil {
			panic(fmt.Sprintf("invalid --port=%d: %v", *port, err))
		}
		c.ListenOn = listenOn
	}

	if *metricsPort > 0 {
		c.Prometheus.Port = *metricsPort
	}
}

func replacePort(addr string, newPort int) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(newPort)), nil
}
