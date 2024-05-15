package slotnotarization

import (
	"sync/atomic"
	"time"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/hive.go/serializer/v2/serix"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/attestation"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/tipselection"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/upgrade"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection"
	"github.com/iotaledger/iota-core/pkg/storage"
	iotago "github.com/iotaledger/iota.go/v4"
)

// Manager is the component that manages the slot commitments.
type Manager struct {
	events        *notarization.Events
	slotMutations *SlotMutations

	workers                      *workerpool.Group
	errorHandler                 func(error)
	acceptedBlockProcessedDetach func()

	attestation         attestation.Attestations
	ledger              ledger.Ledger
	sybilProtection     sybilprotection.SybilProtection
	upgradeOrchestrator upgrade.Orchestrator
	tipSelection        tipselection.TipSelection

	storage *storage.Storage

	acceptedTimeFunc func() time.Time
	apiProvider      iotago.APIProvider

	commitmentMutex syncutils.Mutex

	// ForceCommitMode contains a flag that indicates whether the manager is in force commit mode.
	ForceCommitMode atomic.Bool

	module.Module
}

func NewProvider() module.Provider[*engine.Engine, notarization.Notarization] {
	return module.Provide(func(e *engine.Engine) notarization.Notarization {
		m := NewManager(e.NewSubModule("NotarizationManager"), e)

		e.ConstructedEvent().OnTrigger(func() {
			m.storage = e.Storage
			m.acceptedTimeFunc = e.Clock.Accepted().Time
			m.ledger = e.Ledger
			m.sybilProtection = e.SybilProtection
			m.tipSelection = e.TipSelection
			m.attestation = e.Attestations
			m.upgradeOrchestrator = e.UpgradeOrchestrator

			wpBlocks := m.workers.CreatePool("Blocks", workerpool.WithWorkerCount(1)) // Using just 1 worker to avoid contention

			m.acceptedBlockProcessedDetach = e.Events.AcceptedBlockProcessed.Hook(func(block *blocks.Block) {
				if err := m.notarizeAcceptedBlock(block); err != nil {
					m.errorHandler(ierrors.Wrapf(err, "failed to add accepted block %s to slot", block.ID()))
				}
				m.tryCommitUntil(block.ID().Slot())

				block.SetNotarized()
			}, event.WithWorkerPool(wpBlocks)).Unhook

			e.Events.Notarization.LinkTo(m.events)

			m.slotMutations = NewSlotMutations(e.Storage.Settings().LatestCommitment().Slot())

			m.InitializedEvent().Trigger()
		})

		return m
	})
}

func NewManager(subModule module.Module, engine *engine.Engine) *Manager {
	return options.Apply(&Manager{
		Module:       subModule,
		events:       notarization.NewEvents(),
		workers:      engine.Workers.CreateGroup("NotarizationManager"),
		errorHandler: engine.ErrorHandler("notarization"),
		apiProvider:  engine,
	}, nil, func(m *Manager) {
		m.ShutdownEvent().OnTrigger(m.Shutdown)

		m.ConstructedEvent().Trigger()
	})
}

func (m *Manager) Shutdown() {
	// Alternative 2
	if m.acceptedBlockProcessedDetach != nil {
		m.acceptedBlockProcessedDetach()
	}
	m.workers.Shutdown()

	m.StoppedEvent().Trigger()
}

// tryCommitUntil tries to create slot commitments until the new provided acceptance time.
func (m *Manager) tryCommitUntil(commitUntilSlot iotago.SlotIndex) {
	if !m.ForceCommitMode.Load() {
		if slot := commitUntilSlot; slot > m.storage.Settings().LatestCommitment().Slot() {
			m.tryCommitSlotUntil(slot)
		}
	}
}

func (m *Manager) ForceCommit(slot iotago.SlotIndex) (*model.Commitment, error) {
	m.LogInfo("force commit", "slot", slot)

	if m.ShutdownEvent().WasTriggered() {
		return nil, ierrors.New("notarization manager was stopped")
	}

	// When force committing set acceptance time in TipSelection to the end of the epoch
	// that is LivenessThresholdUpperBound in the future from the committed slot,
	// so that all the unaccepted blocks in force committed slot are orphaned.
	// The value must be at least LivenessThresholdUpperBound in the future.
	// This is to avoid the situation in which future cone of those blocks becomes accepted after force-committing the slot.
	// This would cause issues with consistency as it's impossible to add blocks to a committed slot.
	artificialAcceptanceTime := m.apiProvider.APIForSlot(slot).TimeProvider().SlotEndTime(slot).Add(m.apiProvider.APIForSlot(slot).ProtocolParameters().LivenessThresholdUpperBound())
	m.tipSelection.SetAcceptanceTime(artificialAcceptanceTime)

	commitment, err := m.createCommitment(slot)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to create commitment for slot %d", slot)
	}

	m.LogTrace("forced commitment", "commitmentID", commitment.ID())

	return commitment, nil
}

func (m *Manager) ForceCommitUntil(commitUntilSlot iotago.SlotIndex) error {
	if m.ForceCommitMode.Swap(true) {
		return ierrors.New("force commitment already in progress")
	}

	m.LogInfo("force committing until", "slot", commitUntilSlot)

	for i := m.storage.Settings().LatestCommitment().Slot() + 1; i <= commitUntilSlot; i++ {
		if _, err := m.ForceCommit(i); err != nil {
			return ierrors.Wrapf(err, "failed to force commit slot %d", i)
		}
	}

	m.LogInfo("successfully forced commitment until", "slot", commitUntilSlot)

	m.ForceCommitMode.Store(false)

	return nil
}

// Reset resets the component to a clean state as if it was created at the last commitment.
func (m *Manager) Reset() {
	m.slotMutations.Reset()
}

// IsBootstrapped returns if the Manager finished committing all pending slots up to the current acceptance time.
func (m *Manager) IsBootstrapped() bool {
	// If acceptance time is somewhere in the middle of slot 10, then the latest committable index is 4 (with minCommittableAge=6),
	// because there are 5 full slots and 1 that is still not finished between slot 10 and slot 4.
	// All slots smaller or equal to 4 are committable.
	latestIndex := m.storage.Settings().LatestCommitment().Slot()
	return latestIndex+m.apiProvider.APIForSlot(latestIndex).ProtocolParameters().MinCommittableAge() >= m.apiProvider.APIForSlot(latestIndex).TimeProvider().SlotFromTime(m.acceptedTimeFunc())
}

func (m *Manager) notarizeAcceptedBlock(block *blocks.Block) (err error) {
	if err = m.slotMutations.AddAcceptedBlock(block); err != nil {
		return ierrors.Wrap(err, "failed to add accepted block to slot mutations")
	}

	return m.attestation.AddAttestationFromValidationBlock(block)
}

func (m *Manager) tryCommitSlotUntil(acceptedBlockIndex iotago.SlotIndex) {
	for i := m.storage.Settings().LatestCommitment().Slot() + 1; i <= acceptedBlockIndex; i++ {
		if m.ShutdownEvent().WasTriggered() {
			break
		}

		if !m.isCommittable(i, acceptedBlockIndex) {
			return
		}

		if _, err := m.createCommitment(i); err != nil {
			m.errorHandler(ierrors.Wrapf(err, "failed to create commitment for slot %d", i))

			return
		}
	}
}

func (m *Manager) isCommittable(slot iotago.SlotIndex, acceptedBlockSlot iotago.SlotIndex) bool {
	return slot+m.apiProvider.APIForSlot(slot).ProtocolParameters().MinCommittableAge() <= acceptedBlockSlot
}

func (m *Manager) createCommitment(slot iotago.SlotIndex) (*model.Commitment, error) {
	m.LogDebug("Trying to create commitment for slot", "slot", slot)
	m.commitmentMutex.Lock()
	defer m.commitmentMutex.Unlock()

	latestCommitment := m.storage.Settings().LatestCommitment()
	if slot != latestCommitment.Slot()+1 {
		return nil, ierrors.Errorf("cannot create commitment for slot %d, latest commitment is for slot %d", slot, latestCommitment.Slot())
	}

	acceptedBlocksSet, err := m.slotMutations.Commit(slot)
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to commit acceptedBlocksSet")
	}

	cumulativeWeight, attestationsRoot, err := m.attestation.Commit(slot)
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to commit attestations")
	}

	stateRoot, mutationRoot, accountRoot, created, consumed, mutations, err := m.ledger.CommitSlot(slot)
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to commit ledger")
	}

	committeeRoot, rewardsRoot, err := m.sybilProtection.CommitSlot(slot)
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to commit sybil protection")
	}
	apiForSlot := m.apiProvider.APIForSlot(slot)

	protocolParametersAndVersionsHash, err := m.upgradeOrchestrator.Commit(slot)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to commit protocol parameters and versions in upgrade orchestrator for slot %d", slot)
	}

	roots := iotago.NewRoots(
		acceptedBlocksSet.Root(),
		mutationRoot,
		attestationsRoot,
		stateRoot,
		accountRoot,
		committeeRoot,
		rewardsRoot,
		protocolParametersAndVersionsHash,
	)

	// calculate the new RMC
	rmc, err := m.ledger.RMCManager().CommitSlot(slot)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to commit RMC for slot %d", slot)
	}

	newCommitment := iotago.NewCommitment(
		apiForSlot.ProtocolParameters().Version(),
		slot,
		latestCommitment.ID(),
		roots.ID(),
		cumulativeWeight,
		rmc,
	)

	m.LogInfo("Committing", "commitment", newCommitment, "roots ", roots)

	newModelCommitment, err := model.CommitmentFromCommitment(newCommitment, apiForSlot, serix.WithValidation())
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to create model commitment for commitment %s", newCommitment.MustID())
	}

	rootsStorage, err := m.storage.Roots(slot)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed get roots storage for commitment %s", newModelCommitment.ID())
	}
	if err = rootsStorage.Store(newModelCommitment.ID(), roots); err != nil {
		return nil, ierrors.Wrapf(err, "failed to store latest roots for commitment %s", newModelCommitment.ID())
	}

	if err = m.storage.Commitments().Store(newModelCommitment); err != nil {
		return nil, ierrors.Wrapf(err, "failed to store latest commitment %s", newModelCommitment.ID())
	}

	m.events.SlotCommitted.Trigger(&notarization.SlotCommittedDetails{
		Commitment:            newModelCommitment,
		AcceptedBlocks:        acceptedBlocksSet,
		ActiveValidatorsCount: 0,
		OutputsCreated:        created,
		OutputsConsumed:       consumed,
		Mutations:             mutations,
	})

	if err = m.storage.Settings().SetLatestCommitment(newModelCommitment); err != nil {
		return nil, ierrors.Wrap(err, "failed to set latest commitment")
	}

	// A commitment is considered empty if it has no accepted blocks.
	if acceptedBlocksSet.Size() > 0 {
		if err = m.storage.Settings().AdvanceLatestNonEmptySlot(slot); err != nil {
			return nil, ierrors.Wrap(err, "failed to advance latest non-empty slot")
		}
	}

	m.events.LatestCommitmentUpdated.Trigger(newModelCommitment)

	return newModelCommitment, nil
}

func (m *Manager) AcceptedBlocksCount(index iotago.SlotIndex) int {
	return m.slotMutations.AcceptedBlocksCount(index)
}

var _ notarization.Notarization = new(Manager)
