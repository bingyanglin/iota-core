package slotstore

import (
	"github.com/iotaledger/hive.go/ds/types"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
)

const (
	diffChangePrefix byte = iota
	destroyedAccountsPrefix
)

// AccountDiffs is the storable unit of Account changes for all account in a slot.
type AccountDiffs struct {
	api               iotago.API
	slot              iotago.SlotIndex
	diffChangeStore   *kvstore.TypedStore[iotago.AccountID, *model.AccountDiff]
	destroyedAccounts *kvstore.TypedStore[iotago.AccountID, types.Empty]
}

// NewAccountDiffs creates a new AccountDiffs instance.
func NewAccountDiffs(slot iotago.SlotIndex, store kvstore.KVStore, api iotago.API) *AccountDiffs {
	return &AccountDiffs{
		api:  api,
		slot: slot,
		diffChangeStore: kvstore.NewTypedStore[iotago.AccountID, *model.AccountDiff](lo.PanicOnErr(store.WithExtendedRealm(kvstore.Realm{diffChangePrefix})),
			iotago.AccountID.Bytes,
			iotago.AccountIDFromBytes,
			(*model.AccountDiff).Bytes,
			model.AccountDiffFromBytes,
		),
		destroyedAccounts: kvstore.NewTypedStore[iotago.AccountID, types.Empty](lo.PanicOnErr(store.WithExtendedRealm(kvstore.Realm{destroyedAccountsPrefix})),
			iotago.AccountID.Bytes,
			iotago.AccountIDFromBytes,
			types.Empty.Bytes,
			types.EmptyFromBytes,
		),
	}
}

func (b *AccountDiffs) Slot() iotago.SlotIndex {
	return b.slot
}

// Store stores the given accountID as a root block.
func (b *AccountDiffs) Store(accountID iotago.AccountID, accountDiff *model.AccountDiff, destroyed bool) (err error) {
	if destroyed {
		if err := b.destroyedAccounts.Set(accountID, types.Void); err != nil {
			return ierrors.Wrap(err, "failed to set destroyed account")
		}
	}

	return b.diffChangeStore.Set(accountID, accountDiff)
}

// Load loads accountID and commitmentID for the given blockID.
func (b *AccountDiffs) Load(accountID iotago.AccountID) (accountDiff *model.AccountDiff, destroyed bool, err error) {
	destroyed, err = b.destroyedAccounts.Has(accountID)
	if err != nil {
		return accountDiff, false, ierrors.Wrap(err, "failed to get destroyed account")
	} // load diff for a destroyed account to recreate the state

	accountDiff, err = b.diffChangeStore.Get(accountID)
	if err != nil {
		return accountDiff, false, ierrors.Wrapf(err, "failed to get account diff for account %s", accountID)
	}

	return accountDiff, destroyed, err
}

// Has returns true if the given accountID is a root block.
func (b *AccountDiffs) Has(accountID iotago.AccountID) (has bool, err error) {
	return b.diffChangeStore.Has(accountID)
}

// Delete deletes the given accountID from the root blocks.
func (b *AccountDiffs) Delete(accountID iotago.AccountID) (err error) {
	return b.diffChangeStore.Delete(accountID)
}

func (b *AccountDiffs) Clear() error {
	if err := b.diffChangeStore.Clear(); err != nil {
		return err
	}

	return b.destroyedAccounts.Clear()
}

// Stream streams all accountIDs changes for a slot index.
func (b *AccountDiffs) Stream(consumer func(accountID iotago.AccountID, accountDiff *model.AccountDiff, destroyed bool) bool) error {
	// Existing accounts modified in the slot only have a SlotDiff, but are not part of the b.destroyedAccount store.
	// Destroyed accounts should also have a SlotDiff
	// and be part of b.destroyedAccount so that it's possible to re-create those.

	var internalErr error
	if storageErr := b.diffChangeStore.Iterate(kvstore.EmptyPrefix, func(accountID iotago.AccountID, accountDiff *model.AccountDiff) bool {
		destroyed, err := b.destroyedAccounts.Has(accountID)
		if err != nil {
			internalErr = ierrors.Wrapf(err, "failed to check if an account %s was destroyed", accountID)

			return false
		}

		return consumer(accountID, accountDiff, destroyed)
	}); storageErr != nil {
		return ierrors.Wrapf(storageErr, "failed to iterate over account diffs for slot %s", b.slot)
	} else if internalErr != nil {
		return internalErr
	}

	return nil
}

// StreamDestroyed streams all destroyed accountIDs for a slot index.
func (b *AccountDiffs) StreamDestroyed(consumer func(accountID iotago.AccountID) bool) error {
	return b.destroyedAccounts.Iterate(kvstore.EmptyPrefix, func(accountID iotago.AccountID, _ types.Empty) bool {
		return consumer(accountID)
	})
}
