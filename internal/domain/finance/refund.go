package finance

// RefundInput describes a cancellation in one currency.
type RefundInput struct {
	// Paid is what the customer paid so far.
	Paid int64
	// SupplierCost is what the agency paid (or owes) the supplier.
	SupplierCost int64
	// SupplierPenalty is the cancellation fee the supplier keeps.
	SupplierPenalty int64
	// ServiceFee is the agency's own non-refundable fee.
	ServiceFee int64
}

// RefundSettlement offsets the supplier penalty against the customer
// refund: the customer gets back what they paid minus the penalty and the
// agency fee; the supplier returns the cost minus its penalty.
type RefundSettlement struct {
	CustomerRefund int64
	SupplierRefund int64
	Retained       int64
	// AgencyResult is the agency's gain (positive) or loss on the
	// cancellation once both sides settle.
	AgencyResult int64
	// Shortfall is the penalty part the customer's payment did not cover
	// and still owes.
	Shortfall int64
}

// Validate checks the inputs.
func (in RefundInput) Validate() error {
	f := fields{}
	for k, v := range map[string]int64{
		"paid": in.Paid, "supplier_cost": in.SupplierCost,
		"supplier_penalty": in.SupplierPenalty, "service_fee": in.ServiceFee,
	} {
		if v < 0 || v > MaxMoney {
			f.add(k, "0 or more")
		}
	}
	if in.SupplierPenalty > in.SupplierCost && in.SupplierCost > 0 {
		f.add("supplier_penalty", "cannot exceed the supplier cost")
	}
	return f.err("invalid refund")
}

// SettleRefund computes the offsetting.
func SettleRefund(in RefundInput) RefundSettlement {
	owed := in.SupplierPenalty + in.ServiceFee
	s := RefundSettlement{
		CustomerRefund: max(in.Paid-owed, 0),
		SupplierRefund: max(in.SupplierCost-in.SupplierPenalty, 0),
	}
	s.Retained = in.Paid - s.CustomerRefund
	s.Shortfall = max(owed-in.Paid, 0)
	// Money in: what the customer paid and what the supplier returns.
	// Money out: what was paid to the supplier and what goes back to the customer.
	s.AgencyResult = in.Paid + s.SupplierRefund - in.SupplierCost - s.CustomerRefund
	return s
}
