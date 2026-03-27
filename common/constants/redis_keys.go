package constants

// ============================================================
// Redis Key 规范定义
// 命名格式: {prefix}:{entity}:{identifier}
// ============================================================

const (
	// 用户相关 Key
	RedisKeyPrefixUser      = "user"  // 用户基础信息缓存
	RedisKeyPrefixUserToken = "token" // 用户 Token 黑名单/失效

	// 商品与库存相关 Key
	RedisKeyPrefixProduct      = "product"       // 商品缓存
	RedisKeyPrefixProductStock = "stock"         // 商品实时库存
	RedisKeyPrefixSeckillStock = "seckill:stock" // 秒杀活动库存（预扣减）

	// 秒杀相关 Key
	RedisKeyPrefixSeckillUser  = "seckill:user"  // 用户秒杀购买记录（防重）
	RedisKeyPrefixSeckillOrder = "seckill:order" // 秒杀订单状态（结果查询）
	RedisKeyPrefixSeckillLock  = "seckill:lock"  // 分布式锁

	// 订单相关 Key
	RedisKeyPrefixOrder        = "order"         // 订单缓存
	RedisKeyPrefixOrderIdempot = "order:idempot" // 订单幂等键
)

// ============================================================
// Redis Key 模板（使用 fmt.Sprintf 填充参数）
// ============================================================

var (
	// User Key 模板
	KeyUserInfo       = "user:info:%d"  // user:info:{userId}
	KeyUserTokenBlack = "user:token:%s" // user:token:{token} - 用于 Token 失效

	// Product Key 模板
	KeyProductInfo  = "product:info:%d"  // product:info:{productId}
	KeyProductStock = "product:stock:%d" // product:stock:{productId}

	// Seckill Key 模板
	KeySeckillStock   = "seckill:stock:%d"   // seckill:stock:{seckillProductId} - 库存计数器
	KeySeckillUserBuy = "seckill:user:%d:%d" // seckill:user:{seckillProductId}:{userId} - 防重标记
	KeySeckillOrder   = "seckill:order:%s"   // seckill:order:{orderId} - 订单状态
	KeySeckillLock    = "seckill:lock:%d:%d" // seckill:lock:{seckillProductId}:{userId} - 分布式锁

	// Order Key 模板
	KeyOrderInfo    = "order:info:%s"    // order:info:{orderId}
	KeyOrderIdempot = "order:idempot:%s" // order:idempot:{idempotKey}
)

// ============================================================
// Redis Key TTL 定义（秒）
// ============================================================

const (
	TTLUserInfo     = 3600  // 用户信息缓存 1 小时
	TTLProductInfo  = 600   // 商品信息缓存 10 分钟
	TTLSeckillOrder = 86400 // 秒杀订单状态 24 小时（足够长，支持用户回来查询）
	TTLOrderInfo    = 3600  // 订单信息缓存 1 小时
)

// ============================================================
// RabbitMQ 配置定义
// ============================================================

const (
	RabbitMQExchange   = "seckill_exchange"    // 秒杀交换机
	RabbitMQRoutingKey = "seckill.order"       // 秒杀订单路由键
	RabbitMQQueueOrder = "seckill_order_queue" // 秒杀订单队列
)

// ============================================================
// RabbitMQ Consumer Group 定义
// ============================================================

const (
	RabbitMQConsumerOrder = "order-service-consumer" // 订单服务消费者
)

// ============================================================
// gRPC 服务名称定义（用于服务发现）
// ============================================================

const (
	GrpcServiceUser    = "user-service:8081"
	GrpcServiceProduct = "product-service:8082"
	GrpcServiceSeckill = "seckill-service:8083"
	GrpcServiceOrder   = "order-service:8084"
)

// ============================================================
// HTTP 路由前缀定义
// ============================================================

const (
	HttpPrefixUser    = "/api/v1/user"
	HttpPrefixProduct = "/api/v1/product"
	HttpPrefixSeckill = "/api/v1/seckill"
	HttpPrefixOrder   = "/api/v1/order"
	HttpPrefixHealth  = "/health"
)

// ============================================================
// 分页默认值
// ============================================================

const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// ============================================================
// 秒杀常量
// ============================================================

const (
	SeckillStatusPending = "pending" // 排队中
	SeckillStatusSuccess = "success" // 成功
	SeckillStatusFailed  = "failed"  // 失败

	SeckillResultCodeSuccess          = "SUCCESS"
	SeckillResultCodeSoldOut          = "SOLD_OUT"
	SeckillResultCodeAlreadyPurchased = "ALREADY_PURCHASED"
	SeckillResultCodeNotStarted       = "SECKILL_NOT_STARTED"
	SeckillResultCodeEnded            = "SECKILL_ENDED"
	SeckillResultCodeSystemError      = "SYSTEM_ERROR"
	SeckillResultCodeInvalidRequest   = "INVALID_REQUEST"

	// 秒杀活动状态
	SeckillProductStatusPending = 0 // 未开始
	SeckillProductStatusActive  = 1 // 进行中
	SeckillProductStatusEnded   = 2 // 已结束
)
