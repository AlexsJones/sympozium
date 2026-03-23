package controller

import (
	"testing"

	sympoziumv1alpha1 "github.com/sympozium-ai/sympozium/api/v1alpha1"
	channel "github.com/sympozium-ai/sympozium/internal/channel"
)

func TestCheckChannelAccess(t *testing.T) {
	tests := []struct {
		name        string
		channels    []sympoziumv1alpha1.ChannelSpec
		msg         channel.InboundMessage
		wantAllowed bool
		wantDeny    string
	}{
		{
			name:     "no access control configured",
			channels: []sympoziumv1alpha1.ChannelSpec{{Type: "telegram"}},
			msg:      channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: true,
		},
		{
			name:     "channel type not in instance spec",
			channels: []sympoziumv1alpha1.ChannelSpec{{Type: "slack"}},
			msg:      channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: true,
		},
		{
			name: "allowed sender in list",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedSenders: []string{"123", "789"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: true,
		},
		{
			name: "allowed sender not in list",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedSenders: []string{"789", "012"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
		},
		{
			name: "denied sender in list",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					DeniedSenders: []string{"123"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
		},
		{
			name: "denied sender not in list",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					DeniedSenders: []string{"789"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: true,
		},
		{
			name: "sender in both allow and deny lists - deny wins",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedSenders: []string{"123"},
					DeniedSenders:  []string{"123"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
		},
		{
			name: "allowed chat in list",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedChats: []string{"456", "789"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: true,
		},
		{
			name: "allowed chat not in list",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedChats: []string{"789"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
		},
		{
			name: "allowed chat passes but denied sender blocks",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedChats:  []string{"456"},
					DeniedSenders: []string{"123"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
		},
		{
			name: "deny message returned when set",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedSenders: []string{"789"},
					DenyMessage:    "You are not authorized.",
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
			wantDeny:    "You are not authorized.",
		},
		{
			name: "deny message empty when not set",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedSenders: []string{"789"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: false,
			wantDeny:    "",
		},
		{
			name: "all checks pass",
			channels: []sympoziumv1alpha1.ChannelSpec{{
				Type: "telegram",
				AccessControl: &sympoziumv1alpha1.ChannelAccessControl{
					AllowedSenders: []string{"123"},
					AllowedChats:   []string{"456"},
					DeniedSenders:  []string{"999"},
				},
			}},
			msg:         channel.InboundMessage{Channel: "telegram", SenderID: "123", ChatID: "456"},
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := &sympoziumv1alpha1.SympoziumInstance{
				Spec: sympoziumv1alpha1.SympoziumInstanceSpec{
					Channels: tt.channels,
				},
			}
			allowed, denyMsg := checkChannelAccess(inst, &tt.msg)
			if allowed != tt.wantAllowed {
				t.Errorf("checkChannelAccess() allowed = %v, want %v", allowed, tt.wantAllowed)
			}
			if denyMsg != tt.wantDeny {
				t.Errorf("checkChannelAccess() denyMsg = %q, want %q", denyMsg, tt.wantDeny)
			}
		})
	}
}
