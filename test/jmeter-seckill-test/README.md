# JMeter 秒杀压测配置指南

## 安装 JMeter

1. 下载：https://jmeter.apache.org/download_jmeter.cgi
2. 解压到任意目录
3. 运行：`bin/jmeter.bat` (Windows) 或 `bin/jmeter.sh` (Linux/Mac)

## 创建测试计划

### 1. 添加线程组（Thread Group）
- 右键 Test Plan → Add → Threads → Thread Group
- 配置：
  - Number of Threads: 10000（模拟 10000 个用户）
  - Ramp-up Period: 10（10秒内启动所有用户）
  - Loop Count: 1（每个用户执行 1 次）

### 2. 添加 HTTP 请求（HTTP Request）
- 右键 Thread Group → Add → Sampler → HTTP Request
- 配置：
  - Protocol: http
  - Server Name: localhost
  - Port: 8888
  - Method: POST
  - Path: /seckill/v1/seckill
  - Body Data:
    ```json
    {
      "user_id": ${__Random(10000,1000000)},
      "seckill_product_id": 1,
      "quantity": 1
    }
    ```

### 3. 添加 HTTP Header Manager
- 右键 HTTP Request → Add → Config Element → HTTP Header Manager
- 添加 Header：
  - Content-Type: application/json
  - Authorization: Bearer ${__UUID()}

### 4. 添加监听器（Listener）
- 右键 Thread Group → Add → Listener → Summary Report
- 右键 Thread Group → Add → Listener → Aggregate Report
- 右键 Thread Group → Add → Listener → View Results Tree

### 5. 添加断言（Assertion）
- 右键 HTTP Request → Add → Assertions → JSON Assertion
- 配置：
  - Assert JSON Path: $.code
  - Expected Value: SUCCESS

## 运行测试

### GUI 模式（调试用）
1. 点击绿色三角形按钮
2. 查看 Summary Report

### CLI 模式（正式压测）
```bash
jmeter -n -t seckill_test.jmx -l results.jtl -e -o report/
```

参数说明：
- `-n`: 非 GUI 模式
- `-t`: 测试计划文件
- `-l`: 结果文件
- `-e`: 生成 HTML 报告
- `-o`: 报告输出目录

## 分布式压测

### Master 节点
```bash
jmeter -n -t seckill_test.jmx -R 192.168.1.10,192.168.1.11 -l results.jtl
```

### Slave 节点
```bash
# 在每台 slave 机器上运行
jmeter-server
```

## 查看报告

打开 `report/index.html`，查看：
- TPS（Throughput）
- 响应时间（Response Time）
- 错误率（Error Rate）
- 并发用户数（Active Threads）

## 优势

- ✅ 图形化界面，易于配置
- ✅ 支持分布式压测
- ✅ 自动生成 HTML 报告
- ✅ 支持录制脚本（HTTP(S) Test Script Recorder）
- ✅ 丰富的插件生态

## 劣势

- ❌ 占用资源较多（Java 应用）
- ❌ 单机并发能力有限（~5000）
- ❌ 脚本不如代码灵活
