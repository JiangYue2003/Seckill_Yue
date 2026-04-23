import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import encoding from 'k6/encoding';
import crypto from 'k6/crypto';

// 自定义指标
const successCount = new Counter('seckill_success');
const failCount = new Counter('seckill_fail');
const latency = new Trend('seckill_latency');

// 压测配置
export const options = {
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(95)', 'p(99)'],
  scenarios: {
    // 场景 1：逐步加压（找到系统上限）
    ramp_up: {
      executor: 'ramping-arrival-rate',
      startRate: 100,       // 从 100 req/s 开始
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 2000,
      stages: [
        { target: 500, duration: '30s' },   // 30s 内加压到 500 req/s
        { target: 1000, duration: '30s' },  // 再加压到 1000 req/s
        { target: 1500, duration: '30s' },  // 再加压到 1500 req/s
        { target: 2000, duration: '30s' },  // 最后加压到 2000 req/s
        { target: 2000, duration: '60s' },  // 保持 2000 req/s 持续 1 分钟
      ],
    },
  },

  thresholds: {
    'http_req_duration': ['p(95)<500', 'p(99)<1000'],  // 95% 请求 <500ms
    'seckill_success': ['count>1000'],                  // 至少 1000 个成功
  },
};

// 配置
const GATEWAY_URL = __ENV.GATEWAY_URL || 'http://localhost:8888';
const SECKILL_PRODUCT_ID = __ENV.SECKILL_PRODUCT_ID || '1';
const JWT_SECRET = 'seckill-mall-jwt-secret-key-2026';  // 从 gateway.yaml 获取

// JWT 缓存（LRU 策略，限制大小）
const jwtCache = new Map();
const JWT_CACHE_SIZE = 5000;  // 只缓存 5000 个最近使用的 token

// 生成唯一用户 ID（范围足够大，避免重复）
function generateUserId() {
  return 10000 + Math.floor(Math.random() * 10000000);  // 1000万用户范围
}

// 生成 JWT token（带 LRU 缓存）
function generateJWT(userId) {
  // 1. 检查缓存
  if (jwtCache.has(userId)) {
    const cached = jwtCache.get(userId);
    // 移到最后（LRU 更新）
    jwtCache.delete(userId);
    jwtCache.set(userId, cached);
    return cached;
  }

  // 2. 生成新 token
  const header = {
    alg: 'HS256',
    typ: 'JWT'
  };
  const headerB64 = encoding.b64encode(JSON.stringify(header), 'rawurl');

  const now = Math.floor(Date.now() / 1000);
  const payload = {
    userId: userId,
    exp: now + 3600,  // 1 小时过期
    iat: now,
    jti: `test_${userId}_${now}`
  };
  const payloadB64 = encoding.b64encode(JSON.stringify(payload), 'rawurl');

  const message = `${headerB64}.${payloadB64}`;
  const signature = crypto.hmac('sha256', JWT_SECRET, message, 'binary');
  const signatureB64 = encoding.b64encode(signature, 'rawurl');

  const token = `${headerB64}.${payloadB64}.${signatureB64}`;

  // 3. 写入缓存（LRU 淘汰）
  if (jwtCache.size >= JWT_CACHE_SIZE) {
    // 删除最旧的（Map 的第一个元素）
    const firstKey = jwtCache.keys().next().value;
    jwtCache.delete(firstKey);
  }
  jwtCache.set(userId, token);

  return token;
}

// 主测试函数
export default function () {
  const userId = generateUserId();
  const token = generateJWT(userId);

  const url = `${GATEWAY_URL}/api/v1/seckill`;
  const payload = JSON.stringify({
    seckillProductId: parseInt(SECKILL_PRODUCT_ID),
    quantity: 1,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    timeout: '10s',
  };

  const start = new Date();
  const response = http.post(url, payload, params);
  const duration = new Date() - start;

  latency.add(duration);

  // 检查响应
  const success = check(response, {
    'status is 200': (r) => r.status === 200,
    'response has code': (r) => {
      try {
        return r.json('code') !== undefined;
      } catch (e) {
        return false;
      }
    },
  });

  if (success && response.status === 200) {
    try {
      const body = response.json();
      if (body.code === 0 || body.message === 'success') {
        successCount.add(1);
        console.log(`✅ 秒杀成功: userId=${userId}, orderId=${body.data ? body.data.order_id : 'N/A'}`);
      } else {
        failCount.add(1);
        const code = body.code;
        if (code !== 'SOLD_OUT') {
          console.log(`❌ 秒杀失败: userId=${userId}, code=${code}, msg=${body.message}`);
        }
      }
    } catch (e) {
      failCount.add(1);
      console.log(`❌ 解析响应失败: userId=${userId}, error=${e}`);
    }
  } else {
    failCount.add(1);
    console.log(`❌ 请求失败: userId=${userId}, status=${response.status}, body=${response.body}`);
  }
}

// 测试结束后的汇总
export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
    'summary.json': JSON.stringify(data),
  };
}

function textSummary(data, options) {
  const indent = options.indent || '';
  const totalReqs = getMetricValue(data, 'http_reqs', 'count', 0);
  const success = getMetricValue(data, 'seckill_success', 'count', 0);
  const fail = getMetricValue(data, 'seckill_fail', 'count', 0);
  const avg = getMetricValue(data, 'http_req_duration', 'avg', 0);
  const p50 = getMetricValue(
    data,
    'http_req_duration',
    'p(50)',
    getMetricValue(data, 'http_req_duration', 'med', 0),
  );
  const p95 = getMetricValue(data, 'http_req_duration', 'p(95)', 0);
  const p99 = getMetricValue(data, 'http_req_duration', 'p(99)', 0);
  const tps = getMetricValue(data, 'http_reqs', 'rate', 0);

  let summary = '\n========================================\n';
  summary += '   秒杀压测报告 (k6)\n';
  summary += '========================================\n\n';

  summary += `${indent}总请求数: ${formatInt(totalReqs)}\n`;
  summary += `${indent}成功数: ${formatInt(success)}\n`;
  summary += `${indent}失败数: ${formatInt(fail)}\n\n`;

  summary += `${indent}延迟统计 (ms):\n`;
  summary += `${indent}  平均: ${formatFloat(avg)}\n`;
  summary += `${indent}  P50: ${formatFloat(p50)}\n`;
  summary += `${indent}  P95: ${formatFloat(p95)}\n`;
  summary += `${indent}  P99: ${formatFloat(p99)}\n\n`;

  summary += `${indent}TPS: ${formatFloat(tps)}\n`;
  summary += '========================================\n';

  return summary;
}

function getMetricValue(data, metricName, valueKey, fallback) {
  const metric = data && data.metrics ? data.metrics[metricName] : null;
  const values = metric && metric.values ? metric.values : null;
  const value = values ? values[valueKey] : undefined;
  return Number.isFinite(value) ? value : fallback;
}

function formatFloat(value) {
  return Number.isFinite(value) ? value.toFixed(2) : '0.00';
}

function formatInt(value) {
  return Number.isFinite(value) ? String(Math.round(value)) : '0';
}
