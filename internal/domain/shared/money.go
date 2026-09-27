package shared

import "fmt"

// MinorPerMajor is the scale of every money amount in the API (integer minor units).
const MinorPerMajor = 100

// FormatMinor renders integer minor units as "1250.50 USD" without float math.
func FormatMinor(amount int64, currency string) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, amount/MinorPerMajor, amount%MinorPerMajor, currency)
}
