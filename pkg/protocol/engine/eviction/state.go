package eviction

import (
	"io"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/hive.go/serializer/v2"
	"github.com/iotaledger/hive.go/serializer/v2/stream"

	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/storage/permanent"
	"github.com/iotaledger/iota-core/pkg/storage/prunable/slotstore"
	iotago "github.com/iotaledger/iota.go/v4"
)

// State represents the state of the eviction and keeps track of the root blocks.
type State struct {
	settings             *permanent.Settings
	rootBlockStorageFunc func(iotago.SlotIndex) (*slotstore.Store[iotago.BlockID, iotago.CommitmentID], error)
	lastCommittedSlot    iotago.SlotIndex
	evictionMutex        syncutils.RWMutex
}

// NewState creates a new eviction State.
func NewState(settings *permanent.Settings, rootBlockStorageFunc func(iotago.SlotIndex) (*slotstore.Store[iotago.BlockID, iotago.CommitmentID], error)) (state *State) {
	return &State{
		settings:             settings,
		rootBlockStorageFunc: rootBlockStorageFunc,
	}
}

func (s *State) Initialize(lastCommittedSlot iotago.SlotIndex) {
	// This marks the slot from which we only have root blocks, so starting with 0 is valid here, since we only have a root block for genesis.
	s.lastCommittedSlot = lastCommittedSlot
}

func (s *State) AdvanceActiveWindowToIndex(slot iotago.SlotIndex) {
	s.evictionMutex.Lock()
	defer s.evictionMutex.Unlock()

	if slot <= s.lastCommittedSlot {
		return
	}

	s.lastCommittedSlot = slot
}

func (s *State) LastEvictedSlot() iotago.SlotIndex {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	return s.lastCommittedSlot
}

// BelowOrInActiveRootBlockRange checks if the Block associated with the given id is within or below the active root block range
// with respect to the latest committed slot.
func (s *State) BelowOrInActiveRootBlockRange(id iotago.BlockID) (belowRange bool, inRange bool) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	slot := id.Slot()

	startSlot, endSlot := s.activeIndexRange(s.lastCommittedSlot)

	if slot >= startSlot && slot <= endSlot {
		return false, true
	}

	return slot < startSlot, false
}

func (s *State) AllActiveRootBlocks() map[iotago.BlockID]iotago.CommitmentID {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	activeRootBlocks := make(map[iotago.BlockID]iotago.CommitmentID)
	startSlot, endSlot := s.activeIndexRange(s.lastCommittedSlot)
	for slot := startSlot; slot <= endSlot; slot++ {
		// We assume the cache is always populated for the latest slots.
		storage, err := s.rootBlockStorageFunc(slot)
		// Slot too old, it was pruned.
		if err != nil {
			continue
		}

		_ = storage.Stream(func(id iotago.BlockID, commitmentID iotago.CommitmentID) error {
			activeRootBlocks[id] = commitmentID

			return nil
		})
	}

	return activeRootBlocks
}

func (s *State) LatestActiveRootBlock() (iotago.BlockID, iotago.CommitmentID) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	startSlot, endSlot := s.activeIndexRange(s.lastCommittedSlot)
	for slot := endSlot; slot >= startSlot && slot > 0; slot-- {
		// We assume the cache is always populated for the latest slots.
		storage, err := s.rootBlockStorageFunc(slot)
		// Slot too old, it was pruned.
		if err != nil {
			continue
		}

		var latestRootBlock iotago.BlockID
		var latestSlotCommitmentID iotago.CommitmentID

		_ = storage.Stream(func(id iotago.BlockID, commitmentID iotago.CommitmentID) error {
			latestRootBlock = id
			latestSlotCommitmentID = commitmentID

			// We want the newest rootblock.
			return ierrors.New("stop iteration")
		})

		// We found the most recent root block in this slot.
		if latestRootBlock != iotago.EmptyBlockID {
			return latestRootBlock, latestSlotCommitmentID
		}
	}

	// Fallback to genesis block and genesis commitment if we have no active root blocks.
	return s.settings.APIProvider().CommittedAPI().ProtocolParameters().GenesisBlockID(), model.NewEmptyCommitment(s.settings.APIProvider().CommittedAPI()).ID()
}

// AddRootBlock inserts a solid entry point to the seps map.
func (s *State) AddRootBlock(id iotago.BlockID, commitmentID iotago.CommitmentID) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	// The rootblock is too old, ignore it.
	if id.Slot() < lo.Return1(s.activeIndexRange(s.lastCommittedSlot)) {
		return
	}

	if err := lo.PanicOnErr(s.rootBlockStorageFunc(id.Slot())).Store(id, commitmentID); err != nil {
		panic(ierrors.Wrapf(err, "failed to store root block %s", id))
	}
}

// RemoveRootBlock removes a solid entry points from the map.
func (s *State) RemoveRootBlock(id iotago.BlockID) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	if err := lo.PanicOnErr(s.rootBlockStorageFunc(id.Slot())).Delete(id); err != nil {
		panic(err)
	}
}

// IsActiveRootBlock returns true if the given block is an _active_ root block.
func (s *State) IsActiveRootBlock(id iotago.BlockID) (has bool) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	if !s.withinActiveIndexRange(id.Slot()) {
		return false
	}

	storage, err := s.rootBlockStorageFunc(id.Slot())
	if err != nil {
		return false
	}

	return lo.PanicOnErr(storage.Has(id))
}

// RootBlockCommitmentID returns the commitmentID if it is a known root block, _no matter if active or not_.
func (s *State) RootBlockCommitmentID(id iotago.BlockID) (commitmentID iotago.CommitmentID, exists bool) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	storage, err := s.rootBlockStorageFunc(id.Slot())
	if err != nil {
		return iotago.EmptyCommitmentID, false
	}

	// This return empty value for commitmentID in the case the key is not found.
	commitmentID, exists, err = storage.Load(id)
	if err != nil {
		panic(ierrors.Wrapf(err, "failed to load root block %s", id))
	}

	return commitmentID, exists
}

// Export exports the root blocks to the given writer.
// They not only are needed as a Tangle root on the slot we're targeting to export (usually last committed slot) but also to derive the rootcommitment.
// The rootcommitment, however, must not depend on the committed slot but on the finalized slot. Otherwise, we could never switch a chain after committing (as the rootcommitment is our genesis and we don't solidify/switch chains below it).
func (s *State) Export(writer io.WriteSeeker, targetSlot iotago.SlotIndex) (err error) {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	// The lowerTarget is usually going to be the last finalized slot because Rootblocks are special when creating a snapshot.
	lowerTarget := s.settings.LatestFinalizedSlot()
	if lowerTarget > targetSlot {
		lowerTarget = targetSlot
	}

	start, _ := s.activeIndexRange(lowerTarget)

	latestNonEmptySlot := s.settings.APIProvider().APIForSlot(targetSlot).ProtocolParameters().GenesisSlot()

	if err := stream.WriteCollection(writer, serializer.SeriLengthPrefixTypeAsUint32, func() (elementsCount int, err error) {
		for currentSlot := start; currentSlot <= targetSlot; currentSlot++ {
			storage, err := s.rootBlockStorageFunc(currentSlot)
			if err != nil {
				continue
			}
			if err = storage.StreamBytes(func(rootBlockIDBytes []byte, commitmentIDBytes []byte) (err error) {
				if err = stream.WriteBytes(writer, rootBlockIDBytes); err != nil {
					return ierrors.Wrapf(err, "failed to write root block ID %s", rootBlockIDBytes)
				}

				if err = stream.WriteBytes(writer, commitmentIDBytes); err != nil {
					return ierrors.Wrapf(err, "failed to write root block's %s commitment %s", rootBlockIDBytes, commitmentIDBytes)
				}

				elementsCount++

				latestNonEmptySlot = currentSlot

				return
			}); err != nil {
				return 0, ierrors.Wrap(err, "failed to stream root blocks")
			}
		}

		return elementsCount, nil
	}); err != nil {
		return ierrors.Wrap(err, "failed to write root blocks")
	}

	if err := stream.Write(writer, latestNonEmptySlot); err != nil {
		return ierrors.Wrap(err, "failed to write latest non empty slot")
	}

	return nil
}

// Import imports the root blocks from the given reader.
func (s *State) Import(reader io.ReadSeeker) error {
	if err := stream.ReadCollection(reader, serializer.SeriLengthPrefixTypeAsUint32, func(i int) error {
		rootBlockID, err := stream.Read[iotago.BlockID](reader)
		if err != nil {
			return ierrors.Wrapf(err, "failed to read root block id %d", i)
		}

		commitmentID, err := stream.Read[iotago.CommitmentID](reader)
		if err != nil {
			return ierrors.Wrapf(err, "failed to read root block's %s commitment id %d", rootBlockID, i)
		}

		if err := lo.PanicOnErr(s.rootBlockStorageFunc(rootBlockID.Slot())).Store(rootBlockID, commitmentID); err != nil {
			panic(ierrors.Wrapf(err, "failed to store root block %s", rootBlockID))
		}

		return nil
	}); err != nil {
		return ierrors.Wrap(err, "failed to read root blocks")
	}

	latestNonEmptySlot, err := stream.Read[iotago.SlotIndex](reader)
	if err != nil {
		return ierrors.Wrap(err, "failed to read latest non empty slot")
	}

	if err := s.settings.SetLatestNonEmptySlot(latestNonEmptySlot); err != nil {
		return ierrors.Wrapf(err, "failed to set latest non empty slot to %d", latestNonEmptySlot)
	}

	return nil
}

func (s *State) Rollback(lowerTarget iotago.SlotIndex, targetSlot iotago.SlotIndex) error {
	s.evictionMutex.RLock()
	defer s.evictionMutex.RUnlock()

	start, _ := s.activeIndexRange(lowerTarget)
	latestNonEmptySlot := s.settings.APIProvider().APIForSlot(targetSlot).ProtocolParameters().GenesisSlot()

	for currentSlot := start; currentSlot <= targetSlot; currentSlot++ {
		_, err := s.rootBlockStorageFunc(currentSlot)
		if err != nil {
			continue
		}

		latestNonEmptySlot = currentSlot
	}

	if err := s.settings.SetLatestNonEmptySlot(latestNonEmptySlot); err != nil {
		return ierrors.Wrapf(err, "failed to set latest non empty slot to %d", latestNonEmptySlot)
	}

	return nil
}

func (s *State) Reset() { /* nothing to reset but comply with interface */ }

func (s *State) activeIndexRange(targetSlot iotago.SlotIndex) (startSlot iotago.SlotIndex, endSlot iotago.SlotIndex) {
	protocolParams := s.settings.APIProvider().APIForSlot(targetSlot).ProtocolParameters()
	genesisSlot := protocolParams.GenesisSlot()
	maxCommittableAge := protocolParams.MaxCommittableAge()

	if targetSlot < maxCommittableAge+genesisSlot {
		return genesisSlot, targetSlot
	}

	rootBlocksWindowStart := (targetSlot - maxCommittableAge) + 1

	if latestNonEmptySlot := s.settings.LatestNonEmptySlot(); rootBlocksWindowStart > latestNonEmptySlot {
		rootBlocksWindowStart = latestNonEmptySlot
	}

	return rootBlocksWindowStart, targetSlot
}

func (s *State) withinActiveIndexRange(slot iotago.SlotIndex) bool {
	startSlot, endSlot := s.activeIndexRange(s.lastCommittedSlot)

	return slot >= startSlot && slot <= endSlot
}
