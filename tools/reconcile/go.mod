module github.com/seckill-mall/reconcile-tool

go 1.25.0

require (
	github.com/go-sql-driver/mysql v1.9.3
	github.com/redis/go-redis/v9 v9.17.3
	google.golang.org/grpc v1.79.3
	gopkg.in/yaml.v3 v3.0.1
	seckill-mall/seckill-service v0.0.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace seckill-mall/seckill-service => ../../seckill-service
