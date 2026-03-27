-- ============================================================
-- Seckill Lua 脚本 - 原子性扣减库存
-- ============================================================
-- KEYS[1]: 秒杀库存 Key (seckill:stock:{seckillProductId})
-- KEYS[2]: 用户购买记录 Key (seckill:user:{seckillProductId}:{userId})
-- ARGV[1]: 购买数量
-- ARGV[2]: 订单号
-- ARGV[3]: 过期时间(秒)
-- ARGV[4]: 开始时间戳(秒)
-- ARGV[5]: 结束时间戳(秒)
-- ARGV[6]: 当前时间戳(秒)
-- 返回值:
--   1: 成功
--   0: 库存不足
--  -1: 已购买过
--  -3: 秒杀未开始
--  -4: 秒杀已结束
-- ============================================================

local stockKey = KEYS[1]
local userKey = KEYS[2]
local quantity = tonumber(ARGV[1])
local orderId = ARGV[2]
local ttl = tonumber(ARGV[3])
local startTime = tonumber(ARGV[4])
local endTime = tonumber(ARGV[5])
local now = tonumber(ARGV[6])

-- 0. 时间校验
if startTime > 0 and now < startTime then
    return -3  -- 秒杀未开始
end
if endTime > 0 and now > endTime then
    return -4  -- 秒杀已结束
end

-- 1. 检查用户是否已购买
local alreadyBought = redis.call('EXISTS', userKey)
if alreadyBought == 1 then
    return -1  -- 已购买过
end

-- 2. 检查库存
local currentStock = tonumber(redis.call('GET', stockKey) or 0)
if currentStock < quantity then
    return 0  -- 库存不足
end

-- 3. 扣减库存
local newStock = redis.call('DECRBY', stockKey, quantity)
if newStock < 0 then
    -- 库存扣减为负数，说明库存不足，回滚
    redis.call('INCRBY', stockKey, quantity)
    return 0
end

-- 4. 记录用户购买记录（用于防重）
redis.call('SETEX', userKey, ttl, orderId)

return 1  -- 成功
