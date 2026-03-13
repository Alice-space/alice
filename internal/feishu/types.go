package feishu

import (
	"encoding/json"
	"strings"
)

const (
	TransportKind           = "im_feishu"
	CardActionTransportKind = "im_feishu_card"
	ConversationNamespace   = "feishu"
	MessageInputSchemaID    = "feishu.im.message.v1"
	CardActionInputSchemaID = "feishu.card.action.v1"

	ActionTokenValueKey = "human_action_token"
)

// Config captures the Feishu-specific application settings.
type Config struct {
	Enabled           bool
	AppID             string
	AppSecret         string
	VerificationToken string
	EncryptKey        string
	ReplyInThread     bool
}

func (c Config) EnabledAndConfigured() bool {
	return c.Enabled && strings.TrimSpace(c.AppID) != "" && strings.TrimSpace(c.AppSecret) != "" && strings.TrimSpace(c.VerificationToken) != ""
}

// MessageMetadata stores the transport details needed for later replies.
type MessageMetadata struct {
	EventID       string      `json:"event_id,omitempty"`
	TenantKey     string      `json:"tenant_key,omitempty"`
	MessageID     string      `json:"message_id,omitempty"`
	RootID        string      `json:"root_id,omitempty"`
	ParentID      string      `json:"parent_id,omitempty"`
	ThreadID      string      `json:"thread_id,omitempty"`
	ChatID        string      `json:"chat_id,omitempty"`
	ChatType      string      `json:"chat_type,omitempty"`
	MessageType   string      `json:"message_type,omitempty"`
	RawContent    string      `json:"raw_content,omitempty"`
	SenderOpenID  string      `json:"sender_open_id,omitempty"`
	SenderUserID  string      `json:"sender_user_id,omitempty"`
	SenderUnionID string      `json:"sender_union_id,omitempty"`
	Extra         interface{} `json:"extra,omitempty"`
}

func (m MessageMetadata) ConversationID() string {
	chatID := strings.TrimSpace(m.ChatID)
	if chatID == "" {
		return ""
	}
	return ConversationNamespace + ":" + chatID
}

func (m MessageMetadata) ThreadKey() string {
	if threadID := strings.TrimSpace(m.ThreadID); threadID != "" {
		return threadID
	}
	if rootID := strings.TrimSpace(m.RootID); rootID != "" {
		return rootID
	}
	return "root"
}

func (m MessageMetadata) ReplyTarget(replyInThread bool) ReplyTarget {
	return ReplyTarget{
		MessageID:      strings.TrimSpace(m.MessageID),
		ChatID:         strings.TrimSpace(m.ChatID),
		ThreadID:       strings.TrimSpace(m.ThreadID),
		ReplyInThread:  replyInThread && strings.TrimSpace(m.ThreadID) != "",
		ConversationID: m.ConversationID(),
	}
}

func (m MessageMetadata) ActorRef() string {
	switch {
	case strings.TrimSpace(m.SenderOpenID) != "":
		return "feishu:open_id:" + strings.TrimSpace(m.SenderOpenID)
	case strings.TrimSpace(m.SenderUserID) != "":
		return "feishu:user_id:" + strings.TrimSpace(m.SenderUserID)
	case strings.TrimSpace(m.SenderUnionID) != "":
		return "feishu:union_id:" + strings.TrimSpace(m.SenderUnionID)
	default:
		return ""
	}
}

func (m MessageMetadata) IdempotencyKey() string {
	if eventID := strings.TrimSpace(m.EventID); eventID != "" {
		return "feishu:event:" + eventID
	}
	if messageID := strings.TrimSpace(m.MessageID); messageID != "" {
		return "feishu:message:" + messageID
	}
	return ""
}

func EncodeMetadataPatch(meta MessageMetadata) json.RawMessage {
	if data, err := json.Marshal(meta); err == nil {
		return data
	}
	return nil
}

func DecodeMetadataPatch(raw json.RawMessage) (MessageMetadata, error) {
	var meta MessageMetadata
	if len(raw) == 0 {
		return meta, nil
	}
	err := json.Unmarshal(raw, &meta)
	return meta, err
}

// InboundMessage is the SDK-normalized message callback.
type InboundMessage struct {
	Metadata MessageMetadata
	Text     string
}

// CardAction describes a card callback normalized from the Feishu SDK.
type CardAction struct {
	EventID        string                 `json:"event_id,omitempty"`
	TenantKey      string                 `json:"tenant_key,omitempty"`
	OpenMessageID  string                 `json:"open_message_id,omitempty"`
	OpenChatID     string                 `json:"open_chat_id,omitempty"`
	OperatorOpenID string                 `json:"operator_open_id,omitempty"`
	OperatorUserID string                 `json:"operator_user_id,omitempty"`
	ActionTag      string                 `json:"action_tag,omitempty"`
	ActionName     string                 `json:"action_name,omitempty"`
	Option         string                 `json:"option,omitempty"`
	InputValue     string                 `json:"input_value,omitempty"`
	Value          map[string]interface{} `json:"value,omitempty"`
	FormValue      map[string]interface{} `json:"form_value,omitempty"`
}

func (a CardAction) HumanActionToken() string {
	return stringFromAny(a.Value[ActionTokenValueKey])
}

func (a CardAction) ActorRef() string {
	switch {
	case strings.TrimSpace(a.OperatorOpenID) != "":
		return "feishu:open_id:" + strings.TrimSpace(a.OperatorOpenID)
	case strings.TrimSpace(a.OperatorUserID) != "":
		return "feishu:user_id:" + strings.TrimSpace(a.OperatorUserID)
	default:
		return ""
	}
}

func (a CardAction) SourceRef() string {
	if v := strings.TrimSpace(a.InputValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(stringFromAny(a.Value["source_ref"])); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.ActionName); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.ActionTag); v != "" {
		return v
	}
	return "feishu card action"
}

func (a CardAction) InputPatch() json.RawMessage {
	patch := map[string]interface{}{}
	for k, v := range a.FormValue {
		patch[k] = v
	}
	if strings.TrimSpace(a.InputValue) != "" {
		patch["input_value"] = strings.TrimSpace(a.InputValue)
	}
	if len(patch) == 0 {
		return nil
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil
	}
	return data
}

// CardActionResult is the transport-level response returned to Feishu after a card click.
type CardActionResult struct {
	ToastType    string
	ToastContent string
}

// ReplyTarget identifies where a reply should be sent back in Feishu.
type ReplyTarget struct {
	MessageID      string `json:"message_id,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
	ThreadID       string `json:"thread_id,omitempty"`
	ReplyInThread  bool   `json:"reply_in_thread,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

func (t ReplyTarget) Valid() bool {
	return strings.TrimSpace(t.MessageID) != ""
}

func stringFromAny(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		raw, _ := json.Marshal(x)
		return string(raw)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
