package deposit

import (
	"time"

	"github.com/shopspring/decimal"
)

type Status string
type Provider string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
	StatusCancelled  Status = "CANCELLED"

	ProviderSandbox Provider = "SANDBOX"
	ProviderMTNMomo Provider = "MTN_MOMO"
)

type Deposit struct {
	ID                int64           `json:"id"`
	UserID            int64           `json:"user_id"`
	Amount            decimal.Decimal `json:"amount"`
	Provider          string          `json:"provider"`
	ProviderReference string          `json:"provider_reference"`
	Status            Status          `json:"status"`
	CreatedAt         time.Time       `json:"created_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
}

type CreateRequest struct {
	Amount   string `json:"amount"`
	Provider string `json:"provider"`
}
