package payment

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

var queueCSVHeader = []string{"kind", "booking_id", "booking_ref", "customer_name", "amount", "currency", "status", "due_at", "note"}

// BuildQueueCSV renders queue items as a UTF-8 BOM CSV with every text cell
// neutralized against formula injection.
func BuildQueueCSV(items []QueueItem) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(shared.CSVBOM)
	w := csv.NewWriter(&buf)
	if err := w.Write(queueCSVHeader); err != nil {
		return nil, err
	}
	for _, it := range items {
		due := ""
		if it.DueAt != nil {
			due = it.DueAt.UTC().Format(time.RFC3339)
		}
		n := shared.NeutralizeCSVCell
		// amount is a formatted int64 and stays numeric for spreadsheets.
		row := []string{
			n(string(it.Kind)), it.BookingID.String(), n(it.BookingRef), n(it.CustomerName),
			strconv.FormatInt(it.Amount, 10), n(it.Currency), n(it.Status), due, n(it.Note),
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
