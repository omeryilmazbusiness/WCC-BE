package realtime

import (
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func TestVisibleFailsClosed(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	branch, otherBranch := uuid.New(), uuid.New()
	emp := Subscriber{UserID: me, Scope: access.Scope{Level: access.LevelOwn, UserID: me, BranchID: branch}}
	gm := Subscriber{UserID: other, Scope: access.Scope{Level: access.LevelGlobal, UserID: other}}
	none := Subscriber{UserID: uuid.New()}

	cases := []struct {
		name string
		sub  Subscriber
		sig  Signal
		want bool
	}{
		{"own notification", emp, Signal{UserID: &me}, true},
		{"someone else's notification", emp, Signal{UserID: &other}, false},
		{"own branch invalidation", emp, Signal{BranchID: &branch}, true},
		{"other branch invalidation", emp, Signal{BranchID: &otherBranch}, false},
		{"global signal to branch user", emp, Signal{}, false},
		{"global signal to gm", gm, Signal{}, true},
		{"gm sees every branch", gm, Signal{BranchID: &otherBranch}, true},
		{"no scope sees nothing", none, Signal{BranchID: &branch}, false},
	}
	for _, c := range cases {
		if got := c.sub.Visible(c.sig); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestHubRoutesDropsSlowAndBoundsStreams(t *testing.T) {
	h := NewHub(1, 2)
	a, b := uuid.New(), uuid.New()
	chA, cancelA, ok := h.Subscribe(Subscriber{UserID: a})
	if !ok {
		t.Fatal("subscribe a")
	}
	chB, cancelB, _ := h.Subscribe(Subscriber{UserID: b})
	if _, _, ok := h.Subscribe(Subscriber{UserID: uuid.New()}); ok {
		t.Fatal("hub must refuse streams past its limit")
	}

	h.Broadcast(Signal{Type: TypeNotification, UserID: &a})
	h.Broadcast(Signal{Type: TypeNotification, UserID: &a}) // buffer full: dropped, not blocking
	if got := <-chA; got.UserID == nil || *got.UserID != a {
		t.Fatalf("a got %+v", got)
	}
	select {
	case s := <-chA:
		t.Fatalf("slow stream must drop overflow, got %+v", s)
	case s := <-chB:
		t.Fatalf("b must not receive a's signal, got %+v", s)
	default:
	}

	cancelA()
	cancelA()
	if h.Len() != 1 {
		t.Fatalf("len=%d", h.Len())
	}
	cancelB()
	if h.Len() != 0 {
		t.Fatalf("len=%d", h.Len())
	}
}
