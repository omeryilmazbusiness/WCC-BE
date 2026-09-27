package events_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

func TestDeliverJoinsErrorsAndRecoversPanics(t *testing.T) {
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	ran := 0
	bus.Subscribe(events.TaskOverdue, func(context.Context, events.Event) error { ran++; return errors.New("first") })
	bus.Subscribe(events.TaskOverdue, func(context.Context, events.Event) error { ran++; panic("second") })
	bus.Subscribe(events.TaskOverdue, func(context.Context, events.Event) error { ran++; return nil })
	err := bus.Deliver(context.Background(), events.Event{Name: events.TaskOverdue})
	if ran != 3 || err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("ran=%d err=%v", ran, err)
	}
	if !bus.Subscribed(events.TaskOverdue) || bus.Subscribed(events.ImportCompleted) {
		t.Fatal("Subscribed is wrong")
	}
}

func samplePayloads() map[string]any {
	b, id := uuid.New(), uuid.New()
	return map[string]any{
		events.ConversationMessageReceived: events.ConversationMessageReceivedPayload{ConversationID: id, BranchID: b, Channel: "whatsapp"},
		events.ConversationResponded:       events.ConversationRespondedPayload{ConversationID: id, BranchID: b},
		events.LeadStageChanged:            events.LeadStageChangedPayload{LeadID: id, BranchID: b, From: "new", To: "contacted"},
		events.PaymentReversed:             events.PaymentReversedPayload{PaymentID: id, BranchID: b, Amount: 100, Currency: "USD"},
		events.DocumentStatusChanged:       events.DocumentStatusChangedPayload{DocumentID: id, BranchID: b, To: "approved"},
		events.TaskOverdue:                 events.TaskOverduePayload{TaskID: id, BranchID: b},
		events.TargetStatusChanged:         events.TargetStatusChangedPayload{TargetID: id, BranchID: b, From: "on_track", To: "behind"},
		events.IntegrationFailed:           events.IntegrationFailedPayload{BranchID: b, Source: "webhook", Provider: "meta"},
		events.ImportCompleted:             events.ImportCompletedPayload{ImportJobID: id, BranchID: b, Inserted: 3},
	}
}

func TestDurableRoundTrip(t *testing.T) {
	samples := samplePayloads()
	durable := 0
	for _, e := range events.Catalog() {
		if !e.Durable {
			if events.IsDurable(e.Name) {
				t.Errorf("%s decodes but is not marked durable", e.Name)
			}
			continue
		}
		durable++
		p, ok := samples[e.Name]
		if !ok {
			t.Fatalf("no sample payload for %s", e.Name)
		}
		raw, branch, err := events.Encode(events.Event{Name: e.Name, Payload: p})
		if err != nil || branch == nil {
			t.Fatalf("%s: encode err=%v branch=%v", e.Name, err, branch)
		}
		back, err := events.Decode(e.Name, raw)
		if err != nil || back.Payload != p {
			t.Fatalf("%s: round trip %v != %v (%v)", e.Name, back.Payload, p, err)
		}
	}
	if durable != 9 {
		t.Fatalf("want 9 durable events, got %d", durable)
	}
}

func TestEncodeRejectsWrongPayload(t *testing.T) {
	if _, _, err := events.Encode(events.Event{Name: events.TaskOverdue, Payload: events.ImportCompletedPayload{}}); err == nil {
		t.Fatal("mismatched payload type must be rejected")
	}
	if _, _, err := events.Encode(events.Event{Name: events.LeadCreated, Payload: 1}); err == nil {
		t.Fatal("in-process events must not enter the outbox")
	}
	if _, err := events.Decode("nope", nil); err == nil {
		t.Fatal("unknown event must fail to decode")
	}
}

// TestEveryCatalogEventIsPublished fails when an event is listed in the
// catalog but no production code builds an events.Event with that name.
func TestEveryCatalogEventIsPublished(t *testing.T) {
	consts := catalogConstants(t)
	published := map[string]bool{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(path, filepath.Join("platform", "events")) {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isEventType(lit.Type) {
				return true
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok || !isIdent(kv.Key, "Name") {
					continue
				}
				if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
					published[sel.Sel.Name] = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events.Catalog() {
		ident, ok := consts[e.Name]
		if !ok {
			t.Errorf("catalog event %s has no constant", e.Name)
			continue
		}
		if !published[ident] {
			t.Errorf("catalog event %s (events.%s) is never published", e.Name, ident)
		}
	}
}

func catalogConstants(t *testing.T) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "catalog.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
					v, _ := strconv.Unquote(lit.Value)
					out[v] = name.Name
				}
			}
		}
	}
	return out
}

func isEventType(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Event" && isIdent(sel.X, "events")
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}
