package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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
	logx.MustSetup(c.Log)
	defer logx.Close()
	logutil.SetupInstanceFields(c.Log.ServiceName, *port)
	ctx := svc.NewServiceContext(c)

	// 启动主处理队列消费者
	if ctx.Consumer != nil {
		if err := ctx.Consumer.Start(); err != nil {
			logx.Errorf("RocketMQ order consumer failed to start: %v", err)
		}
	}
	// 启动超时检查队列消费者
	if ctx.CheckConsumer != nil {
		if err := ctx.CheckConsumer.Start(); err != nil {
			logx.Errorf("RocketMQ check consumer failed to start: %v", err)
		}
	}
	// 启动死信队列监控消费者
	if ctx.DLQConsumer != nil {
		if err := ctx.DLQConsumer.Start(); err != nil {
			logx.Errorf("RocketMQ DLQ consumer failed to start: %v", err)
		}
	}

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		order.RegisterOrderServiceServer(grpcServer, server.NewOrderServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	shutdownDone := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logx.Info("received shutdown signal, stopping service...")

		if ctx.Consumer != nil {
			_ = ctx.Consumer.Stop()
		}
		if ctx.CheckConsumer != nil {
			_ = ctx.CheckConsumer.Stop()
		}
		if ctx.DLQConsumer != nil {
			_ = ctx.DLQConsumer.Stop()
		}
		ctx.Stop()
		s.Stop()

		time.Sleep(2 * time.Second)
		logx.Info("shutdown complete, exiting...")
		close(shutdownDone)
		os.Exit(0)
	}()

	logx.Infof("Starting rpc server at %s...", c.ListenOn)
	s.Start()

	<-shutdownDone
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
