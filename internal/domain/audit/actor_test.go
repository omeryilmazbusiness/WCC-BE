package audit

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestResolveActorContextUserWinsOverInput(t *testing.T) {
	user, session, spoofed := uuid.New(), uuid.New(), uuid.New()
	ctx := WithActor(context.Background(), Actor{
		Type: ActorUser, UserID: user, SessionID: session, IP: "10.0.0.1", UserAgent: "ua", RequestID: "req-1",
	})
	got := ResolveActor(ctx, RecordInput{ActorID: spoofed, IP: "1.2.3.4", UserAgent: "fake"})
	if got.Type != ActorUser || got.UserID == nil || *got.UserID != user {
		t.Fatalf("actor: %+v", got)
	}
	if got.SessionID == nil || *got.SessionID != session || got.IP != "10.0.0.1" || got.UserAgent != "ua" || got.RequestID != "req-1" {
		t.Fatalf("request metadata: %+v", got)
	}
}

func TestResolveActorFallbacks(t *testing.T) {
	legacy := uuid.New()

	got := ResolveActor(context.Background(), RecordInput{ActorID: legacy, IP: "1.2.3.4"})
	if got.Type != ActorUser || *got.UserID != legacy || got.IP != "1.2.3.4" || got.SessionID != nil {
		t.Fatalf("legacy input actor: %+v", got)
	}

	got = ResolveActor(context.Background(), RecordInput{})
	if got.Type != ActorSystem || got.UserID != nil {
		t.Fatalf("no actor: %+v", got)
	}

	hook := WithActor(context.Background(), Actor{Type: ActorWebhook, IP: "5.6.7.8", RequestID: "r"})
	got = ResolveActor(hook, RecordInput{})
	if got.Type != ActorWebhook || got.UserID != nil || got.IP != "5.6.7.8" || got.RequestID != "r" {
		t.Fatalf("webhook: %+v", got)
	}

	anon := WithActor(context.Background(), Actor{IP: "9.9.9.9", RequestID: "login"})
	got = ResolveActor(anon, RecordInput{ActorID: legacy})
	if got.Type != ActorUser || *got.UserID != legacy || got.IP != "9.9.9.9" || got.RequestID != "login" {
		t.Fatalf("unauthenticated request with input actor: %+v", got)
	}
}

func TestAsSystemDropsUserKeepsRequestID(t *testing.T) {
	ctx := WithActor(context.Background(), Actor{Type: ActorUser, UserID: uuid.New(), RequestID: "req"})
	got := ResolveActor(AsSystem(ctx), RecordInput{})
	if got.Type != ActorSystem || got.UserID != nil || got.RequestID != "req" {
		t.Fatalf("system: %+v", got)
	}
	owner := uuid.New()
	got = ResolveActor(AsSystem(ctx), RecordInput{ActorID: owner})
	if got.Type != ActorSystem || *got.UserID != owner {
		t.Fatalf("system on behalf of user: %+v", got)
	}
}
