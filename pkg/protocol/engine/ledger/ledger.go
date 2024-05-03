package ledger

import (
	"io"

	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts/mana"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/congestioncontrol/rmc"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/spenddag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Ledger interface {
	AttachTransaction(block *blocks.Block) (signedTransactionMetadata mempool.SignedTransactionMetadata, containsTransaction bool)
	OnTransactionAttached(callback func(transactionMetadata mempool.TransactionMetadata), opts ...event.Option) *event.Hook[func(metadata mempool.TransactionMetadata)]
	TransactionMetadata(id iotago.TransactionID) (transactionMetadata mempool.TransactionMetadata, exists bool)
	TransactionMetadataByAttachment(blockID iotago.BlockID) (transactionMetadata mempool.TransactionMetadata, exists bool)

	Account(accountID iotago.AccountID, targetSlot iotago.SlotIndex) (accountData *accounts.AccountData, exists bool, err error)
	PastAccounts(accountIDs iotago.AccountIDs, targetSlot iotago.SlotIndex) (pastAccountsData map[iotago.AccountID]*accounts.AccountData, err error)
	AddAccount(account *utxoledger.Output, credits iotago.BlockIssuanceCredits) error

	Output(id iotago.OutputID) (*utxoledger.Output, error)
	OutputOrSpent(id iotago.OutputID) (output *utxoledger.Output, spent *utxoledger.Spent, err error)
	ForEachUnspentOutput(consumer func(output *utxoledger.Output) bool) error
	AddGenesisUnspentOutput(unspentOutput *utxoledger.Output) error

	SpendDAG() spenddag.SpendDAG[iotago.TransactionID, mempool.StateID, BlockVoteRank]
	MemPool() mempool.MemPool[BlockVoteRank]
	SlotDiffs(slot iotago.SlotIndex) (*utxoledger.SlotDiff, error)

	ManaManager() *mana.Manager
	RMCManager() *rmc.Manager

	CommitSlot(slot iotago.SlotIndex) (stateRoot, mutationRoot, accountRoot iotago.Identifier, created utxoledger.Outputs, consumed utxoledger.Spents, mutations []*iotago.Transaction, err error)

	Import(reader io.ReadSeeker) error
	Export(writer io.WriteSeeker, targetSlot iotago.SlotIndex) error
	TrackBlock(block *blocks.Block)

	AccountRoot() iotago.Identifier

	// Reset resets the component to a clean state as if it was created at the last commitment.
	Reset()

	module.Module
}
