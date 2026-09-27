package audit

import (
	"context"

	"github.com/google/uuid"
)

// ActorType classifies who performed an audited action.
type ActorType string

const (
	ActorUser    ActorType = "user"
	ActorSystem  ActorType = "system"
	ActorWebhook ActorType = "webhook"
)

func (t ActorType) Valid() bool {
	switch t {
	case ActorUser, ActorSystem, ActorWebhook:
		return true
	default:
		return false
	}
}

// Actor is the request (or job) identity the audit trail attributes events
// to. HTTP middleware sets it from the verified access token, so services
// cannot spoof the actor of a user request.
type Actor struct {
	Type      ActorType
	UserID    uuid.UUID
	SessionID uuid.UUID
	BranchID  uuid.UUID
	IP        string
	UserAgent string
	RequestID string
}

// Authenticated reports whether the actor is a verified user.
func (a Actor) Authenticated() bool {
	return a.Type == ActorUser && a.UserID != uuid.Nil
}

type actorKey struct{}

func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// AsSystem marks ctx as platform work (event reactors, jobs). The request id
// is kept so reactor events correlate with the request that triggered them.
func AsSystem(ctx context.Context) context.Context {
	a, _ := ActorFrom(ctx)
	return WithActor(ctx, Actor{Type: ActorSystem, RequestID: a.RequestID})
}

// Resolved is the attribution stored with an event.
type Resolved struct {
	Type      ActorType
	UserID    *uuid.UUID
	SessionID *uuid.UUID
	IP        string
	UserAgent string
	RequestID string
}

// ResolveActor attributes an event. A verified user on ctx always wins over
// in.ActorID; in.ActorID is used only when ctx carries no user (login before
// a token exists, legacy/system callers acting on behalf of a user).
func ResolveActor(ctx context.Context, in RecordInput) Resolved {
	a, ok := ActorFrom(ctx)
	out := Resolved{IP: in.IP, UserAgent: in.UserAgent}
	if ok {
		out.RequestID = a.RequestID
		if a.IP != "" {
			out.IP = a.IP
		}
		if a.UserAgent != "" {
			out.UserAgent = a.UserAgent
		}
	}
	switch {
	case ok && a.Authenticated():
		uid := a.UserID
		out.Type, out.UserID = ActorUser, &uid
		if a.SessionID != uuid.Nil {
			sid := a.SessionID
			out.SessionID = &sid
		}
	case in.ActorID != uuid.Nil:
		uid := in.ActorID
		out.UserID = &uid
		out.Type = ActorUser
		if ok && a.Type.Valid() && a.Type != ActorUser {
			out.Type = a.Type
		}
	case ok && a.Type.Valid() && a.Type != ActorUser:
		out.Type = a.Type
	default:
		out.Type = ActorSystem
	}
	return out
}
