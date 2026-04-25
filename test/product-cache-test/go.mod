module seckill-mall/product-cache-test

go 1.25.0

require (
	github.com/redis/go-redis/v9 v9.17.3
	google.golang.org/grpc v1.79.3
	seckill-mall/common v0.0.0-00010101000000-000000000000
)

replace seckill-mall/common => ../../common

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
