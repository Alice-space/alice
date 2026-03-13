package bus

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

// TestWeatherQueryDirectAnswer 测试案例1：查询上海明天天气
//
// 场景：人类发来自然语言消息"查询上海明天天气"
// 特点：简单、一次性、只读、不需要代码仓库写权限、不需要审批
// 预期：留在 EphemeralRequest 内直接完成，不升级成 DurableTask
//
// 运行方式：
//
//	go test ./internal/bus/... -run TestWeatherQueryDirectAnswer -v
//
// 查看日志：
//
//	tail -f data/test_weather/eventlog/events.*.jsonl | jq .
func TestWeatherQueryDirectAnswer(t *testing.T) {
	// 使用固定目录以便查看日志文件
	dataDir := "./data/test_weather"

	// 默认清理旧数据，避免幂等键命中历史数据导致测试假失败。
	// 如需保留日志用于人工排查，可设置 ALICE_KEEP_WEATHER_LOGS=1。
	if os.Getenv("ALICE_KEEP_WEATHER_LOGS") != "1" {
		_ = os.RemoveAll(dataDir)
	}

	// 创建目录
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	t.Logf("📁 测试数据目录: %s", dataDir)
	t.Logf("📁 事件日志: %s/eventlog/", dataDir)

	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: dataDir, SnapshotInterval: 100})
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	t.Log("✅ Store 初始化完成")

	runtime := newTestRuntime(t, st)
	t.Log("✅ Runtime 初始化完成")

	// 模拟 Reception 产出"留在 EphemeralRequest 内直接完成"的决策
	reception := fixedReception{decision: domain.PromotionDecision{
		DecisionID:             "dec_weather_001",
		IntentKind:             "weather_query",
		RiskLevel:              "low",
		ExternalWrite:          false, // 只读
		CreatePersistentObject: false, // 不创建持久化对象
		Async:                  false, // 同步完成
		MultiStep:              false, // 单步完成
		MultiAgent:             false, // 不需要多 agent
		ApprovalRequired:       false, // 不需要审批
		BudgetRequired:         false, // 不需要预算
		RecoveryRequired:       false, // 不需要恢复机制
		Result:                 domain.PromotionResultDirectAnswer,
		ReasonCodes:            []string{"read_only_query", "low_risk"},
		Confidence:             0.95,
		ProducedBy:             "reception.static",
		ProducedAt:             time.Now().UTC(),
	}}

	t.Log("📝 Step 1: 发送天气查询请求...")

	// Step 1: 入口层接收消息，生成 ExternalEvent
	result, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "im",
		TransportKind:  "webhook",
		SourceRef:      "查询上海明天天气",
		ActorRef:       "user://alice",
		ConversationID: "conv_weather_001",
		ThreadID:       "root",
		ReceivedAt:     time.Now().UTC(),
		IdempotencyKey: "msg_weather_001",
		Verified:       true,
	}, reception)

	if err != nil {
		t.Fatalf("❌ 失败: %v", err)
	}

	t.Logf("✅ Step 1 完成: RequestID=%s", result.RequestID)
	t.Logf("   TaskID=%s (应该为空)", result.TaskID)
	t.Logf("   Promoted=%v (应该为false)", result.Promoted)

	// 验证：创建了 Request，没有创建 Task
	if result.RequestID == "" {
		t.Fatal("❌ expected RequestID to be set")
	}
	if result.TaskID != "" {
		t.Fatalf("❌ weather query should not create DurableTask, got TaskID=%s", result.TaskID)
	}
	if result.Promoted {
		t.Fatal("❌ weather query should not be promoted to DurableTask")
	}

	// Step 2-3: 验证事件
	t.Log("🔍 Step 2: 检查已记录的事件...")

	var events []domain.EventEnvelope
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	hasRequestOpened := false
	hasPromotionAssessed := false
	var requestID string

	for _, evt := range events {
		switch evt.EventType {
		case domain.EventTypeEphemeralRequestOpened:
			hasRequestOpened = true
			requestID = evt.AggregateID
			t.Logf("   📨 EphemeralRequestOpened: %s", requestID)
		case domain.EventTypePromotionAssessed:
			hasPromotionAssessed = true
			t.Logf("   📊 PromotionAssessed: direct_answer")
		}
	}

	if !hasRequestOpened {
		t.Fatal("❌ expected EphemeralRequestOpened event")
	}
	if !hasPromotionAssessed {
		t.Fatal("❌ expected PromotionAssessed event")
	}

	// Step 4-7: 模拟 agent 调用 tool
	t.Log("📝 Step 3: 模拟天气查询 ToolCall...")

	toolCallPayload, _ := json.Marshal(domain.ToolCallRecordedPayload{
		CallID:      "call_weather_001",
		OwnerKind:   "request",
		OwnerID:     requestID,
		ToolOrMCP:   "public_info_query",
		RequestRef:  "query://weather/shanghai/tomorrow",
		ResponseRef: "result://weather/sunny/15-22c",
		Status:      "success",
		StartedAt:   time.Now().UTC(),
		FinishedAt:  time.Now().UTC(),
	})

	toolCallEvent := domain.EventEnvelope{
		EventID:         "evt_toolcall_001",
		AggregateKind:   domain.AggregateKindRequest,
		AggregateID:     requestID,
		EventType:       domain.EventTypeToolCallRecorded,
		Sequence:        3,
		GlobalHLC:       "2026-03-11T10:00:00.000000000Z#0003",
		ProducedAt:      time.Now().UTC(),
		Producer:        "agent.weather",
		PayloadSchemaID: "event.tool_call_recorded",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         toolCallPayload,
	}

	if err := st.AppendBatch(ctx, []domain.EventEnvelope{toolCallEvent}); err != nil {
		t.Fatalf("failed to append tool call event: %v", err)
	}
	t.Log("✅ Step 3 完成: ToolCallRecorded")

	// Step 8-10: 生成回复并结束
	t.Log("📝 Step 4: 生成回复并结束请求...")

	replyPayload, _ := json.Marshal(domain.ReplyRecordedPayload{
		ReplyID:        "rpl_weather_001",
		OwnerKind:      "request",
		OwnerID:        requestID,
		ReplyChannel:   "im",
		ReplyToEventID: "evt_weather_001",
		PayloadRef:     "reply://上海明天晴天，15-22°C，适宜出行",
		Final:          true,
		DeliveredAt:    time.Now().UTC(),
	})

	terminalPayload, _ := json.Marshal(domain.TerminalResultRecordedPayload{
		ResultID:     "res_weather_001",
		OwnerKind:    "request",
		OwnerID:      requestID,
		FinalStatus:  "answered",
		FinalReplyID: "rpl_weather_001",
		ClosedAt:     time.Now().UTC(),
	})

	answeredPayload, _ := json.Marshal(domain.RequestAnsweredPayload{
		RequestID:    requestID,
		FinalReplyID: "rpl_weather_001",
		AnsweredAt:   time.Now().UTC(),
	})

	finalEvents := []domain.EventEnvelope{
		{
			EventID:         "evt_reply_001",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     requestID,
			EventType:       domain.EventTypeReplyRecorded,
			Sequence:        4,
			GlobalHLC:       "2026-03-11T10:00:01.000000000Z#0004",
			ProducedAt:      time.Now().UTC(),
			Producer:        "agent.reception",
			PayloadSchemaID: "event.reply_recorded",
			PayloadVersion:  domain.DefaultPayloadVersion,
			Payload:         replyPayload,
		},
		{
			EventID:         "evt_terminal_001",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     requestID,
			EventType:       domain.EventTypeTerminalResultRecorded,
			Sequence:        5,
			GlobalHLC:       "2026-03-11T10:00:02.000000000Z#0005",
			ProducedAt:      time.Now().UTC(),
			Producer:        "bus",
			PayloadSchemaID: "event.terminal_result_recorded",
			PayloadVersion:  domain.DefaultPayloadVersion,
			Payload:         terminalPayload,
		},
		{
			EventID:         "evt_answered_001",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     requestID,
			EventType:       domain.EventTypeRequestAnswered,
			Sequence:        6,
			GlobalHLC:       "2026-03-11T10:00:03.000000000Z#0006",
			ProducedAt:      time.Now().UTC(),
			Producer:        "bus",
			PayloadSchemaID: "event.request_answered",
			PayloadVersion:  domain.DefaultPayloadVersion,
			Payload:         answeredPayload,
		},
	}

	if err := st.AppendBatch(ctx, finalEvents); err != nil {
		t.Fatalf("failed to append final events: %v", err)
	}

	// 最终验证
	t.Log("🔍 Step 5: 最终验证...")

	events = nil
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	checks := map[string]bool{
		"ToolCall":       false,
		"Reply":          false,
		"TerminalResult": false,
		"Answered":       false,
	}

	for _, evt := range events {
		switch evt.EventType {
		case domain.EventTypeToolCallRecorded:
			checks["ToolCall"] = true
		case domain.EventTypeReplyRecorded:
			checks["Reply"] = true
		case domain.EventTypeTerminalResultRecorded:
			checks["TerminalResult"] = true
		case domain.EventTypeRequestAnswered:
			checks["Answered"] = true
		}
	}

	allPassed := true
	for name, passed := range checks {
		status := "❌"
		if passed {
			status = "✅"
		}
		t.Logf("   %s %s", status, name)
		if !passed {
			allPassed = false
		}
	}

	// 验证没有创建 DurableTask
	var hasTaskCreated bool
	for _, evt := range events {
		if evt.EventType == domain.EventTypeTaskPromotedAndBound {
			hasTaskCreated = true
			break
		}
	}
	if hasTaskCreated {
		t.Log("   ❌ DurableTask (不应该创建)")
		allPassed = false
	} else {
		t.Log("   ✅ 没有创建 DurableTask")
	}

	// 打印事件日志文件位置
	eventLogPath := filepath.Join(dataDir, "eventlog")
	if entries, err := os.ReadDir(eventLogPath); err == nil {
		t.Logf("\n📁 事件日志文件 (%d 个):", len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				t.Logf("   - %s/eventlog/%s", dataDir, entry.Name())
			}
		}
	}

	// 打印查看命令
	t.Log("\n📖 查看事件日志:")
	t.Logf("   cat %s/eventlog/*.jsonl | jq .", dataDir)
	t.Log("\n📖 查看特定事件类型:")
	t.Logf("   cat %s/eventlog/*.jsonl | jq 'select(.event_type==\"EphemeralRequestOpened\")'", dataDir)

	if !allPassed {
		t.Fatal("\n❌ 测试失败")
	}

	t.Log("\n✅ 测试通过！")
	t.Logf("   Request ID: %s", requestID)
	t.Log("   路径: EphemeralRequest -> ToolCall -> Reply -> Answered")
	t.Log("   没有创建 DurableTask（符合直接回答路径预期）")
}

// TestWeatherQueryMidRequestChange 测试中途修改请求
func TestWeatherQueryMidRequestChange(t *testing.T) {
	dataDir := "./data/test_weather_change"

	if os.Getenv("ALICE_KEEP_WEATHER_LOGS") != "1" {
		_ = os.RemoveAll(dataDir)
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	t.Logf("📁 测试数据目录: %s", dataDir)

	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: dataDir, SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	runtime := newTestRuntime(t, st)
	reception := fixedReception{decision: domain.PromotionDecision{
		IntentKind:    "weather_query",
		RiskLevel:     "low",
		ExternalWrite: false,
		Result:        domain.PromotionResultDirectAnswer,
		ReasonCodes:   []string{"read_only_query"},
		Confidence:    0.95,
		ProducedAt:    time.Now().UTC(),
	}}

	t.Log("📝 发送第一个消息: 查询上海明天天气")
	result1, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "im",
		SourceRef:      "查询上海明天天气",
		ConversationID: "conv_weather_002",
		ThreadID:       "root",
		IdempotencyKey: "msg_shanghai",
		ReceivedAt:     time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("✅ Request1: %s", result1.RequestID)

	t.Log("📝 发送第二个消息: 改查北京后天")
	result2, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "im",
		SourceRef:      "改查北京后天",
		ConversationID: "conv_weather_002",
		ThreadID:       "root",
		IdempotencyKey: "msg_beijing",
		ReceivedAt:     time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("✅ Request2: %s", result2.RequestID)

	if result2.RequestID != result1.RequestID {
		t.Logf("⚠️ 第二个消息创建了新的 Request（第一个可能已关闭）")
	} else {
		t.Logf("✅ 第二个消息命中了同一个 Request")
	}

	t.Logf("\n📁 查看日志: cat %s/eventlog/*.jsonl | jq .", dataDir)
}

// TestWeatherQueryHelp 打印帮助信息
func TestWeatherQueryHelp(t *testing.T) {
	t.Log("天气查询测试帮助:")
	t.Log("")
	t.Log("运行测试:")
	t.Log("  go test ./internal/bus/... -run TestWeatherQueryDirectAnswer -v")
	t.Log("")
	t.Log("查看事件日志:")
	t.Log("  cat ./data/test_weather/eventlog/*.jsonl | jq .")
	t.Log("")
	t.Log("查看特定事件:")
	t.Log("  cat ./data/test_weather/eventlog/*.jsonl | jq 'select(.event_type==\"EphemeralRequestOpened\")'")
	t.Log("")
	t.Log("清理测试数据:")
	t.Log("  rm -rf ./data/test_weather")
	t.Log("  rm -rf ./data/test_weather_change")
}
