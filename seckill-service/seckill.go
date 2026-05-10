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
	"seckill-mall/common/seckill"
	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/server"
	"seckill-mall/seckill-service/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/seckill.yaml", "the config file")
var port = flag.Int("port", 0, "override rpc listen port, e.g. --port=19083")
var metricsPort = flag.Int("metrics-port", 0, "override prometheus port, e.g. --metrics-port=19183")

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
	logutil.SetupInstanceFields(c.Log.ServiceName, *port)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		seckill.RegisterSeckillServiceServer(grpcServer, server.NewSeckillServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	// 启动优雅关闭监听（捕获 SIGINT / SIGTERM）
	shutdownDone := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logx.Info("received shutdown signal, stopping service...")

		ctx.Stop() // 先停止 ServiceContext（关闭 MQ、Redis）
		s.Stop()   // 再停止 gRPC 服务

		// 等待 2 秒让 gRPC 完成清理
		time.Sleep(2 * time.Second)
		logx.Info("shutdown complete, exiting...")
		close(shutdownDone)
		os.Exit(0) // 正常退出
	}()

	logx.Infof("Starting rpc server at %s...", c.ListenOn)
	s.Start() // 阻塞直到 s.Stop() 被调用

	// 等待关闭完成
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
