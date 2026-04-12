import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import encoding from 'k6/encoding';
import crypto from 'k6/crypto';

const successCount = new Counter('seckill_success');
const failCount = new Counter('seckill_fail');

// 固定并发，持续压测（找到真实 TPS 上限）
export const options = {
  scenarios: {
    constant_load: {
      executor: 'constant-vus',
      vus: 400,  // 固定 200 并发
      duration: '30s',  // 持续 30 秒
    },
  },
};

const GATEWAY_URL = __ENV.GATEWAY_URL || 'http://localhost:8888';
const SECKILL_PRODUCT_ID = __ENV.SECKILL_PRODUCT_ID || '1';
const JWT_SECRET = 'seckill-mall-jwt-secret-key-2026';

const jwtCache = new Map();
const JWT_CACHE_SIZE = 5000;

function generateUserId() {
  return 10000 + Math.floor(Math.random() * 10000000);
}

function generateJWT(userId) {
  if (jwtCache.has(userId)) {
    const cached = jwtCache.get(userId);
    jwtCache.delete(userId);
    jwtCache.set(userId, cached);
    return cached;
  }

  const header = { alg: 'HS256', typ: 'JWT' };
  const headerB64 = encoding.b64encode(JSON.stringify(header), 'rawurl');

  const now = Math.floor(Date.now() / 1000);
  const payload = {
    userId: userId,
    exp: now + 3600,
    iat: now,
    jti: `test_${userId}_${now}`
  };
  const payloadB64 = encoding.b64encode(JSON.stringify(payload), 'rawurl');

  const message = `${headerB64}.${payloadB64}`;
  const signature = crypto.hmac('sha256', JWT_SECRET, message, 'binary');
  const signatureB64 = encoding.b64encode(signature, 'rawurl');

  const token = `${headerB64}.${payloadB64}.${signatureB64}`;

  if (jwtCache.size >= JWT_CACHE_SIZE) {
    const firstKey = jwtCache.keys().next().value;
    jwtCache.delete(firstKey);
  }
  jwtCache.set(userId, token);

  return token;
}

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

  const response = http.post(url, payload, params);

  check(response, {
    'status is 200': (r) => r.status === 200,
  });

  if (response.status === 200) {
    try {
      const body = response.json();
      if (body.code === 0 || body.message === 'success') {
        successCount.add(1);
      } else {
        failCount.add(1);
      }
    } catch (e) {
      failCount.add(1);
    }
  } else {
    failCount.add(1);
  }
}
