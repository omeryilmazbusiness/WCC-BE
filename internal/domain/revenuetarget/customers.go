package revenuetarget

import "fmt"

// MaxTargetCustomers caps a volume goal so a typo cannot create an absurd target.
const MaxTargetCustomers = 1_000_000

// NormalizeTargetCustomers accepts a nil (no volume goal) or a positive count.
func NormalizeTargetCustomers(n *int) (*int, error) {
	if n == nil {
		return nil, nil
	}
	if *n <= 0 {
		return nil, fmt.Errorf("target_customers must be > 0")
	}
	if *n > MaxTargetCustomers {
		return nil, fmt.Errorf("target_customers is too large")
	}
	v := *n
	return &v, nil
}

// CombineStatus picks the worse of money and customer pace so a dual goal
// reports behind when either side is behind.
func CombineStatus(money, customers Status) Status {
	rank := func(s Status) int {
		switch s {
		case StatusBehind:
			return 3
		case StatusOnTrack:
			return 2
		case StatusAhead:
			return 1
		default:
			return 0
		}
	}
	if rank(customers) > rank(money) {
		return customers
	}
	return money
}
