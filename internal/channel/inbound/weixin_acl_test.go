package inbound

import (
	"context"
	"log/slog"
	"testing"

	"github.com/felinics/memoh/internal/acl"
	"github.com/felinics/memoh/internal/agent/turn"
	"github.com/felinics/memoh/internal/channel"
	"github.com/felinics/memoh/internal/channel/identities"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type denyingChatACLQueries struct {
	dbstore.Queries
}

func (*denyingChatACLQueries) EvaluateBotACLRule(context.Context, sqlc.EvaluateBotACLRuleParams) (string, error) {
	return acl.EffectDeny, nil
}

func TestWeixinUnboundSenderBypassesChatACL(t *testing.T) {
	for _, platform := range []channel.ChannelType{"weixin", "qq", "wechatoa", "telegram"} {
		t.Run(platform.String(), func(t *testing.T) {
			const botID = "11111111-1111-1111-1111-111111111111"
			identitySvc := &fakeChannelIdentityService{
				channelIdentity: identities.ChannelIdentity{ID: "55555555-5555-5555-5555-555555555555"},
			}
			chatSvc := &fakeChatService{resolveResult: route.ResolveConversationResult{BotID: botID, RouteID: "route-1"}}
			gateway := &fakeChatGateway{resp: fakeChatResponse{
				Messages: []turn.ModelMessage{{Role: "assistant", Content: turn.NewTextContent("AI reply")}},
			}}
			processor := NewChannelInboundProcessor(slog.Default(), nil, chatSvc, chatSvc, gateway, identitySvc, &fakePolicyService{}, "", 0)
			processor.SetACLService(acl.NewService(nil, &denyingChatACLQueries{}))
			sender := &fakeReplySender{}
			cfg := channel.ChannelConfig{TeamID: "team-test", ID: "cfg-1", BotID: botID, ChannelType: platform}
			msg := channel.InboundMessage{
				BotID: botID, Channel: platform,
				Message: channel.Message{Text: "hello"}, ReplyTarget: "sender-1",
				Sender:       channel.Identity{SubjectID: "sender-1"},
				Conversation: channel.Conversation{ID: "sender-1", Type: channel.ConversationTypePrivate},
			}

			if err := processor.HandleInbound(context.Background(), cfg, msg, sender); err != nil {
				t.Fatalf("HandleInbound: %v", err)
			}
			if platform == "weixin" {
				if gateway.gotReq.Query != "hello" || gateway.gotReq.UserID != "" {
					t.Fatalf("unbound WeChat sender should reach the agent: %+v", gateway.gotReq)
				}
				if len(sender.sent) != 1 || sender.sent[0].Message.PlainText() != "AI reply" {
					t.Fatalf("expected agent reply without a linking prompt: %+v", sender.sent)
				}
			} else if gateway.gotReq.Query != "" {
				t.Fatal("other channels must still be denied by chat ACL")
			}
		})
	}
}
