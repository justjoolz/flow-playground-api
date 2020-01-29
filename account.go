package flow_playground_api

import "github.com/google/uuid"

type Account struct {
	ID           uuid.UUID
	Address      Address
	DraftCode    string
	DeployedCode string
}
