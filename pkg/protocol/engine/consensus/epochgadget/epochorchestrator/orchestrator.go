package epochorchestrator

import (
	"fmt"
	"io"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/epochgadget"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/epochgadget/epochorchestrator/performance"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/sybilprotection"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Orchestrator struct {
	events            *epochgadget.Events
	sybilProtection   sybilprotection.SybilProtection // do we need the whole SybilProtection or just a callback to RotateCommittee?
	ledger            ledger.Ledger                   // do we need the whole Ledger or just a callback to retrieve account data?
	lastCommittedSlot iotago.SlotIndex
	timeProvider      *iotago.TimeProvider

	performanceManager *performance.Tracker

	epochEndNearingThreshold iotago.SlotIndex
	maxCommittableSlot       iotago.SlotIndex

	optsEpochZeroCommittee *account.Accounts

	mutex syncutils.Mutex

	module.Module
}

func NewProvider(opts ...options.Option[Orchestrator]) module.Provider[*engine.Engine, epochgadget.Gadget] {
	return module.Provide(func(e *engine.Engine) epochgadget.Gadget {
		return options.Apply(&Orchestrator{
			events: epochgadget.NewEvents(),

			// TODO: the following fields should be initialized after the engine is constructed,
			//  otherwise we implicitly rely on the order of engine initialization which can change at any time.
			sybilProtection: e.SybilProtection,
			ledger:          e.Ledger,
		}, opts,
			func(o *Orchestrator) {
				e.HookConstructed(func() {
					e.Storage.Settings().HookInitialized(func() {
						o.timeProvider = e.API().TimeProvider()

						o.epochEndNearingThreshold = e.Storage.Settings().ProtocolParameters().EpochNearingThreshold

						o.performanceManager = performance.NewTracker(e.Storage.Rewards(), e.Storage.PoolStats(), e.Storage.Committee(), e.Storage.PerformanceFactors, e.API().TimeProvider(), e.API().ManaDecayProvider())
						o.lastCommittedSlot = e.Storage.Settings().LatestCommitment().Index()
						o.maxCommittableSlot = e.Storage.Settings().ProtocolParameters().EvictionAge + e.Storage.Settings().ProtocolParameters().EvictionAge

						if o.optsEpochZeroCommittee != nil {
							if err := o.performanceManager.RegisterCommittee(0, o.optsEpochZeroCommittee); err != nil {
								panic(ierrors.Wrap(err, "error while registering initial committee for epoch 0"))
							}
						}

						o.TriggerConstructed()
						o.TriggerInitialized()
					})
				})

				// TODO: does this potentially cause a data race due to fanning-in parallel events?
				// TODO: introduce a mutex so that slotFinalized and slotCommitted do not race on RegisterCommittee.
				// TODO: consider what happens when slot committed and slot finalized are triggered at the same time,
				//  but are executed in different order on different nodes, thus resulting in a different committee being selected on those nodes. How to prevent that?
				e.Events.BlockGadget.BlockAccepted.Hook(o.BlockAccepted)
				e.Events.Notarization.SlotCommitted.Hook(func(scd *notarization.SlotCommittedDetails) { o.CommitSlot(scd.Commitment.Index()) })

				e.Events.SlotGadget.SlotFinalized.Hook(o.slotFinalized)

				e.Events.EpochGadget.LinkTo(o.events)
			},
		)
	})
}

func (o *Orchestrator) Shutdown() {
	o.TriggerStopped()
}

func (o *Orchestrator) BlockAccepted(block *blocks.Block) {
	o.performanceManager.BlockAccepted(block)
}

func (o *Orchestrator) CommitSlot(slot iotago.SlotIndex) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	currentEpoch := o.timeProvider.EpochFromSlot(slot)
	o.lastCommittedSlot = slot

	if o.timeProvider.EpochEnd(currentEpoch) == slot+o.maxCommittableSlot {
		committee, exists := o.performanceManager.LoadCommitteeForEpoch(currentEpoch)
		if !exists {
			// If the committee for the epoch wasn't set before, we promote the current one.
			committee, exists = o.performanceManager.LoadCommitteeForEpoch(currentEpoch - 1)
			if !exists {
				panic(fmt.Sprintf("committee for previous epoch %d not found", currentEpoch-1))
			}

			if err := o.performanceManager.RegisterCommittee(currentEpoch, committee); err != nil {
				panic(ierrors.Wrap(err, "failed to register committee for epoch"))
			}
		}
		o.performanceManager.ApplyEpoch(currentEpoch, committee)
	}
}

func (o *Orchestrator) ValidatorReward(validatorID iotago.AccountID, stakeAmount iotago.BaseToken, epochStart, epochEnd iotago.EpochIndex) (validatorReward iotago.Mana, err error) {
	return o.performanceManager.ValidatorReward(validatorID, stakeAmount, epochStart, epochEnd)
}

func (o *Orchestrator) DelegatorReward(validatorID iotago.AccountID, delegatedAmount iotago.BaseToken, epochStart, epochEnd iotago.EpochIndex) (delegatorsReward iotago.Mana, err error) {
	return o.performanceManager.DelegatorReward(validatorID, delegatedAmount, epochStart, epochEnd)
}

func (o *Orchestrator) Import(reader io.ReadSeeker) error {
	return o.performanceManager.Import(reader)
}

func (o *Orchestrator) Export(writer io.WriteSeeker, targetSlot iotago.SlotIndex) error {
	return o.performanceManager.Export(writer, targetSlot)
}

func (o *Orchestrator) slotFinalized(slot iotago.SlotIndex) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	epoch := o.timeProvider.EpochFromSlot(slot)

	// Only select new committee if the finalized slot is epochEndNearingThreshold slots from EpochEnd and the last
	// committed slot is earlier than the last slot of the epoch.
	// Otherwise, skip committee selection because it's too late and the committee has been reused.
	if slot+o.epochEndNearingThreshold == o.timeProvider.EpochEnd(epoch) && o.timeProvider.EpochEnd(epoch) > o.lastCommittedSlot+o.maxCommittableSlot {
		newCommittee := o.selectNewCommittee(slot)
		o.events.CommitteeSelected.Trigger(newCommittee)
	}
}

func (o *Orchestrator) selectNewCommittee(slot iotago.SlotIndex) *account.Accounts {
	currentEpoch := o.timeProvider.EpochFromSlot(slot)
	nextEpoch := currentEpoch + 1
	candidates := o.performanceManager.EligibleValidatorCandidates(nextEpoch)

	weightedCandidates := account.NewAccounts()
	if err := candidates.ForEach(func(candidate iotago.AccountID) error {
		a, exists, err := o.ledger.Account(candidate, slot)
		if err != nil {
			return err
		}
		if !exists {
			panic("account does not exist")
		}

		weightedCandidates.Set(candidate, &account.Pool{
			PoolStake:      a.ValidatorStake + a.DelegationStake,
			ValidatorStake: a.ValidatorStake,
			FixedCost:      a.FixedCost,
		})

		return nil
	}); err != nil {
		panic(err)
	}

	newCommittee := o.sybilProtection.RotateCommittee(nextEpoch, weightedCandidates)
	weightedCommittee := newCommittee.Accounts()

	err := o.performanceManager.RegisterCommittee(nextEpoch, weightedCommittee)
	if err != nil {
		panic("failed to register committee for epoch")
	}

	return weightedCommittee
}

// WithCommitteeForEpochZero registers the passed committee on a given slot. This is needed to generate Genesis snapshot with some initial committee.
func WithCommitteeForEpochZero(committee *account.Accounts) options.Option[Orchestrator] {
	return func(o *Orchestrator) {
		o.optsEpochZeroCommittee = committee
	}
}
