package mempool

import (
	iotago "github.com/iotaledger/iota.go/v4"
)

// Transaction is the type that is used to describe instructions how to modify the ledger state.
type Transaction interface {
	// ID returns the identifier of the Transaction.
	ID() (iotago.TransactionID, error)

	// Inputs returns the inputs of the Transaction.
	Inputs() ([]Input, error)

	// String returns a human-readable version of the Transaction.
	String() string
}

type Input interface {
	ID() iotago.OutputID
}
