package hotel

// RedactCost hides firma prices, markup and profit from readers who must
// only see the satış (selling) price.
func RedactCost(h *Hotel) {
	h.Markup = Markup{Kind: MarkupPercent, Value: 0}
}

func RedactSummary(s *Summary) {
	RedactCost(&s.Hotel)
	s.FromNet = 0
}

func RedactSeason(s *Season) {
	s.Markup = nil
	for i := range s.Rates {
		s.Rates[i].Single = 0
		s.Rates[i].Double = 0
		s.Rates[i].Triple = 0
		s.Rates[i].Quad = 0
	}
}

func RedactMonthly(r *MonthlyRate) { r.Net = 0 }

func RedactQuote(q *Quote) {
	q.NetTotal = 0
	q.Profit = 0
	for i := range q.NightsDetail {
		q.NightsDetail[i].Net = 0
	}
	for i := range q.Children {
		q.Children[i].Net = 0
	}
}
