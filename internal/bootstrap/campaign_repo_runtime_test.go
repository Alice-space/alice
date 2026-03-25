package bootstrap

import "testing"

func TestCampaignIDFromAutomationStateKey(t *testing.T) {
	tests := []struct {
		name     string
		stateKey string
		wantID   string
		wantOK   bool
	}{
		{
			name:     "dispatch task",
			stateKey: "campaign_dispatch:camp_demo:executor:T001:x1",
			wantID:   "camp_demo",
			wantOK:   true,
		},
		{
			name:     "wake task",
			stateKey: "campaign_wake:camp_demo:T001:2026-03-25T10:00:00Z",
			wantID:   "camp_demo",
			wantOK:   true,
		},
		{
			name:     "unknown campaign",
			stateKey: "campaign_dispatch:unknown:executor:T001:x1",
			wantOK:   false,
		},
		{
			name:     "non campaign task",
			stateKey: "automation:other",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := campaignIDFromAutomationStateKey(tt.stateKey)
			if gotOK != tt.wantOK {
				t.Fatalf("unexpected ok state: got=%v want=%v", gotOK, tt.wantOK)
			}
			if gotID != tt.wantID {
				t.Fatalf("unexpected campaign id: got=%q want=%q", gotID, tt.wantID)
			}
		})
	}
}
