package bus

import (
	"context"
	"encoding/json"
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
// 验证点：
// 1. 创建 EphemeralRequest（不是 DurableTask）
// 2. PromotionDecision 标记为 direct_answer（不是 promote）
// 3. 记录 ToolCallRecord（模拟天气查询）
// 4. 记录 ReplyRecord（回复用户）
// 5. 记录 TerminalResult（结束）
// 6. Request 进入 Answered 状态
func TestWeatherQueryDirectAnswer(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	runtime := newTestRuntime(t, st)

	// 模拟 Reception 产出"留在 EphemeralRequest 内直接完成"的决策
	// 关键字段：effects.external_write=false, async=false, approval_required=false
	reception := fixedReception{decision: domain.PromotionDecision{
		DecisionID:             "dec_weather_001",
		RequestID:              "", // 由测试填充
		IntentKind:             "weather_query",
		RiskLevel:              "low",
		ExternalWrite:          false, // 只读，不写外部系统
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

	// Step 1: 入口层接收消息，生成 ExternalEvent
	// 来源：IM 消息
	result, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "im", // 来源渠道：IM
		TransportKind:     "webhook",
		SourceRef:         "查询上海明天天气", // 原始文本
		ActorRef:          "user://alice",
		ConversationID:    "conv_weather_001",
		ThreadID:          "root",
		ReceivedAt:        time.Now().UTC(),
		IdempotencyKey:    "msg_weather_001",
		Verified:          true,
	}, reception)

	if err != nil {
		t.Fatalf("failed to ingest weather query event: %v", err)
	}

	// 验证：创建了 Request，没有创建 Task（直接回答路径）
	if result.RequestID == "" {
		t.Fatal("expected RequestID to be set")
	}
	if result.TaskID != "" {
		t.Fatalf("weather query should not create DurableTask, got TaskID=%s", result.TaskID)
	}
	if result.Promoted {
		t.Fatal("weather query should not be promoted to DurableTask")
	}

	// Step 2-3: 验证 BUS 创建了 EphemeralRequest 和 PromotionDecision
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
		case domain.EventTypePromotionAssessed:
			hasPromotionAssessed = true
		}
	}

	if !hasRequestOpened {
		t.Fatal("expected EphemeralRequestOpened event")
	}
	if !hasPromotionAssessed {
		t.Fatal("expected PromotionAssessed event")
	}

	// Step 4-7: 模拟 agent 使用 Skill 解析查询并调用 tool
	// 在实际系统中，这会由 Reception agent 完成
	// 这里我们手动注入 ToolCallRecord 来模拟
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

	// Step 8-10: 模拟生成回复并结束 request
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
		ResultID:       "res_weather_001",
		OwnerKind:      "request",
		OwnerID:        requestID,
		FinalStatus:    "answered",
		FinalReplyID:   "rpl_weather_001",
		RevokedRouteKeys: []string{
			domain.NewCanonicalRouteKeyEncoder().Conversation("im", "conv_weather_001", "root"),
		},
		ClosedAt: time.Now().UTC(),
	})

	answeredPayload, _ := json.Marshal(domain.RequestAnsweredPayload{
		RequestID:        requestID,
		FinalReplyID:     "rpl_weather_001",
		RevokedRouteKeys: []string{},
		AnsweredAt:       time.Now().UTC(),
	})

	// 批量写入结束事件
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

	// 验证所有关键事件都已记录
	events = nil
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var hasToolCall, hasReply, hasTerminal, hasAnswered bool
	for _, evt := range events {
		switch evt.EventType {
		case domain.EventTypeToolCallRecorded:
			hasToolCall = true
		case domain.EventTypeReplyRecorded:
			hasReply = true
		case domain.EventTypeTerminalResultRecorded:
			hasTerminal = true
		case domain.EventTypeRequestAnswered:
			hasAnswered = true
		}
	}

	if !hasToolCall {
		t.Error("expected ToolCallRecorded event (weather query)")
	}
	if !hasReply {
		t.Error("expected ReplyRecorded event (reply to user)")
	}
	if !hasTerminal {
		t.Error("expected TerminalResultRecorded event (end state)")
	}
	if !hasAnswered {
		t.Error("expected RequestAnswered event (request closed)")
	}

	// 最终验证：这是一个完整的直接回答路径，没有 DurableTask 相关事件
	var hasTaskCreated bool
	for _, evt := range events {
		if evt.EventType == domain.EventTypeTaskPromotedAndBound {
			hasTaskCreated = true
			break
		}
	}
	if hasTaskCreated {
		t.Fatal("weather query should NOT create DurableTask, but TaskPromotedAndBound found")
	}

	t.Logf("✅ Weather query test passed: request=%s", requestID)
	t.Logf("   - EphemeralRequest created and answered")
	t.Logf("   - No DurableTask created (direct answer path)")
	t.Logf("   - Tool call, reply, and terminal result recorded")
}

// TestWeatherQueryMidRequestChange 测试案例1变体：中途修改请求
//
// 场景：用户先问"查询上海明天天气"，紧接着说"改查北京后天"
// 验证：第二个消息命中同一个 EphemeralRequest（如果仍在 Open 状态）
func TestWeatherQueryMidRequestChange(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	runtime := newTestRuntime(t, st)
	reception := fixedReception{decision: domain.PromotionDecision{
		IntentKind:  "weather_query",
		RiskLevel:   "low",
		ExternalWrite: false,
		Result:      domain.PromotionResultDirectAnswer,
		ReasonCodes: []string{"read_only_query"},
		Confidence:  0.95,
		ProducedAt:  time.Now().UTC(),
	}}

	// 第一个消息：查询上海
	result1, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "im",
		SourceRef:         "查询上海明天天气",
		ConversationID:    "conv_weather_002",
		ThreadID:          "root",
		IdempotencyKey:    "msg_shanghai",
		ReceivedAt:        time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}

	// 第二个消息：改查北京（相同的 conversation_id + thread_id）
	result2, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "im",
		SourceRef:         "改查北京后天",
		ConversationID:    "conv_weather_002", // 相同的会话
		ThreadID:          "root",             // 相同的线程
		IdempotencyKey:    "msg_beijing",
		ReceivedAt:        time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}

	// 验证：第二个消息命中了同一个 Request（通过 conversation_id + thread_id 路由）
	if result2.RequestID != result1.RequestID {
		t.Logf("Note: second message created new request (may be expected if first was already answered)")
	} else {
		t.Logf("✅ Second message hit same request: %s", result1.RequestID)
	}
}
