package connector

import "testing"

func TestProcessorBuildLLMRunEnv_IncludesSessionKey(t *testing.T) {
	processor := &Processor{}
	job := Job{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_chat",
		SourceMessageID: "om_source",
		SenderUserID:    "ou_actor",
		ChatType:        "group",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
	}

	env := processor.buildLLMRunEnv(job)
	if env["ALICE_MCP_SESSION_KEY"] != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected session key env: %#v", env)
	}
}
