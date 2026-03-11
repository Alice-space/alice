# Test 测试指南

本文档说明如何运行 Alice 项目的测试，以及如何查看和理解日志。

## 目录结构

```
test/
├── README.md          # 本文档
├── integration/       # 集成测试（待添加）
└── fixtures/          # 测试数据（待添加）

# 单元测试与实现代码放在同目录
internal/
├── bus/*_test.go      # BUS 层测试
├── domain/*_test.go   # 领域模型测试
├── store/*_test.go    # 存储层测试
└── ...
```

## 运行测试

### 1. 运行所有测试

```bash
cd /Users/alice/Developer/alice
go test ./...
```

### 2. 运行特定包的测试

```bash
# 只运行 BUS 层测试
go test ./internal/bus/...

# 只运行领域模型测试
go test ./internal/domain/...

# 只运行存储层测试
go test ./internal/store/...
```

### 3. 运行特定测试函数

```bash
# 运行天气查询测试（案例1）
go test ./internal/bus/... -run TestWeatherQueryDirectAnswer -v

# 运行直接回答路径测试
go test ./internal/bus/... -run TestDirectAnswer -v

# 运行所有 BUS 层测试并显示详细输出
go test ./internal/bus/... -v
```

### 4. 常用测试参数

```bash
# -v: 显示详细输出（包括 t.Log 信息）
# -count=1: 禁用缓存，强制重新运行
# -timeout=30s: 设置超时时间
# -run=正则: 只运行匹配的测试函数

go test ./internal/bus/... -v -count=1 -timeout=30s -run TestWeatherQuery
```

## 典型案例测试

### 案例1：天气查询（直接回答路径）

测试文件：`internal/bus/weather_query_test.go`

```bash
# 运行天气查询测试
go test ./internal/bus/... -run TestWeatherQueryDirectAnswer -v

# 运行中途修改请求测试
go test ./internal/bus/... -run TestWeatherQueryMidRequestChange -v
```

**测试验证点：**
1. ✅ 创建 EphemeralRequest（不是 DurableTask）
2. ✅ PromotionDecision 标记为 direct_answer
3. ✅ 记录 ToolCallRecord（天气查询调用）
4. ✅ 记录 ReplyRecord（回复用户）
5. ✅ 记录 TerminalResult（结束状态）
6. ✅ Request 进入 Answered 状态

## 查看日志

### 1. 测试日志（控制台输出）

运行测试时添加 `-v` 参数：

```bash
go test ./internal/bus/... -run TestWeatherQueryDirectAnswer -v
```

你会看到类似输出：
```
=== RUN   TestWeatherQueryDirectAnswer
    weather_query_test.go:225: ✅ Weather query test passed: request=req_01HQ...
    weather_query_test.go:226:    - EphemeralRequest created and answered
    weather_query_test.go:227:    - No DurableTask created (direct answer path)
    weather_query_test.go:228:    - Tool call, reply, and terminal result recorded
--- PASS: TestWeatherQueryDirectAnswer (0.05s)
```

### 2. 事件日志（JSONL 格式）

Alice 使用 JSONL 事件日志作为系统真源。测试时会生成临时数据目录：

```bash
# 测试数据目录（临时，每次测试后自动清理）
# 由 t.TempDir() 生成，路径类似：
# /var/folders/.../T/.../data/eventlog/
```

事件日志文件位置（如果测试保留了数据）：
```
data/
└── eventlog/
    └── events.000001.jsonl   # 事件日志文件
```

查看事件日志：
```bash
# 查看最后10条事件
tail -n 10 data/eventlog/events.000001.jsonl | jq .

# 查看特定类型的事件
cat data/eventlog/events.000001.jsonl | jq 'select(.event_type == "EphemeralRequestOpened")'
```

### 3. 应用日志（结构化日志）

Alice 使用 zerolog 输出结构化日志，格式为 JSON。

**控制台日志：**
运行服务时，日志输出到 stdout/stderr：
```bash
# 启动服务并查看日志
./alice serve

# 或以开发模式启动（带彩色输出）
go run ./cmd/alice serve
```

**日志级别：**
```bash
# 配置日志级别（在 configs/alice.yaml 中）
logging:
  level: "debug"    # debug, info, warn, error, fatal
  format: "text"    # json 或 text
  console: true     # 输出到控制台
```

**日志字段说明：**
```json
{
  "timestamp": "2026-03-11T10:00:00.000Z",
  "level": "info",
  "component": "bus",
  "msg": "request answered",
  "request_id": "req_01HQ...",
  "trace_id": "trace_..."
}
```

### 4. 关键日志点

| 阶段 | 日志键 | 说明 |
|------|--------|------|
| 入口 | `event_received` | 接收到外部事件 |
| 路由 | `route_resolved` | 路由解析结果 |
| 决策 | `promotion_decision` | Promotion决定 |
| 执行 | `agent_dispatched` | Agent调用 |
| 执行 | `agent_response` | Agent响应 |
| 存储 | `events_appended` | 事件追加 |
| 错误 | `operation_failed` | 操作失败 |

## 调试技巧

### 1. 查看测试中的事件流

```bash
# 运行测试并打印所有事件
go test ./internal/bus/... -run TestWeatherQueryDirectAnswer -v 2>&1 | grep -E "(Event|request|Task)"
```

### 2. 检查事件日志文件

修改测试以保留数据目录：
```go
// 在测试中使用固定目录而不是 t.TempDir()
st, err := store.Open(store.Config{RootDir: "./test-data"})
```

然后查看：
```bash
# 查看事件日志
cat ./test-data/eventlog/events.*.jsonl | jq .

# 统计事件类型
cat ./test-data/eventlog/events.*.jsonl | jq -r '.event_type' | sort | uniq -c
```

### 3. 使用 delve 调试

```bash
# 安装 delve
go install github.com/go-delve/delve/cmd/dlv@latest

# 调试特定测试
dlv test ./internal/bus/... -- -test.run TestWeatherQueryDirectAnswer

# 在 delve 中
(dlv) break weather_query_test.go:100
(dlv) continue
(dlv) print result
```

## 测试数据固定（Fixture）

测试使用的固定数据：

| 数据 | 位置 | 说明 |
|------|------|------|
| Workflow 定义 | `configs/workflows/` | issue-delivery, research-exploration 等 |
| 配置模板 | `configs/config.example.yaml` | 示例配置 |
| Skill 文件 | `skills/` | Agent Skill 定义 |

## 持续集成

```bash
# 运行所有测试（CI 使用）
go test ./... -count=1

# 运行带竞态检测的测试
go test ./... -race

# 生成覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## 常见问题

**Q: 测试超时怎么办？**
```bash
# 增加超时时间
go test ./... -timeout=5m
```

**Q: 如何只运行集成测试？**
```bash
# 使用 build tag
go test ./... -tags=integration
```

**Q: 如何查看测试生成的临时文件？**
```bash
# 在测试中使用 t.Log 打印临时目录
# 或者修改测试使用固定目录
```

---

更多测试案例请参考：
- `internal/bus/runtime_test.go` - BUS 核心流程测试
- `internal/bus/weather_query_test.go` - 天气查询测试（案例1）
- `docs/cdr/typical_cases.md` - 典型场景设计文档
