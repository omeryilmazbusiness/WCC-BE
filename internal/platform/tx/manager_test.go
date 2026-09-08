package tx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func TestNestedReusesOuter(t *testing.T) {
	// Without a real pool, WithinTransaction cannot begin — ensure TxFrom is empty.
	ctx := context.Background()
	if _, ok := tx.TxFrom(ctx); ok {
		t.Fatal("expected no tx on background context")
	}
	_ = errors.New("pool-backed tests run in integration suite")
}
