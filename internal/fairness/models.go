package fairness

import "github.com/shopspring/decimal"

type CrashResult struct {
	CrashPoint decimal.Decimal
	Hash       string
}

type Config struct {
	// HouseEdge is expressed as a decimal.
	//
	// Examples:
	// 0.00 = 0%
	// 0.03 = 3%
	// 0.05 = 5%
	HouseEdge decimal.Decimal
}
