package endpoint

import "github.com/kazuph/goterrace/internal/api"

const DecisionGeneration = 3

type DecisionProjection struct {
	BootID   string         `json:"boot_id"`
	Revision uint64         `json:"revision"`
	Pending  []api.Decision `json:"pending"`
}
