package ledgertests

import (
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger"
	iotago "github.com/iotaledger/iota.go/v4"
)

type MockedState struct {
	id           iotago.OutputID
	output       *MockedOutput
	creationSlot iotago.SlotIndex
}

func NewMockedState(transactionID iotago.TransactionID, index uint16) *MockedState {
	return &MockedState{
		id:           iotago.OutputIDFromTransactionIDAndIndex(transactionID, index),
		output:       &MockedOutput{},
		creationSlot: iotago.SlotIndex(0),
	}
}

func (m *MockedState) StateID() iotago.Identifier {
	return iotago.IdentifierFromData(lo.PanicOnErr(m.id.Bytes()))
}

func (m *MockedState) Type() utxoledger.StateType {
	return utxoledger.StateTypeUTXOInput
}

func (m *MockedState) IsReadOnly() bool {
	return false
}

func (m *MockedState) OutputID() iotago.OutputID {
	return m.id
}

func (m *MockedState) Output() iotago.Output {
	return m.output
}

func (m *MockedState) SlotCreated() iotago.SlotIndex {
	return m.creationSlot
}

func (m *MockedState) String() string {
	return "MockedOutput(" + m.id.ToHex() + ")"
}
