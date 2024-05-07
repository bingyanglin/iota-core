package sybilprotectionv1

import (
	"io"
	"sort"

	"github.com/iotaledger/hive.go/ads"
	"github.com/iotaledger/hive.go/core/safemath"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/seatmanager"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/seatmanager/topstakers"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/sybilprotectionv1/performance"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

type SybilProtection struct {
	events *sybilprotection.Events

	apiProvider iotago.APIProvider

	seatManager       seatmanager.SeatManager
	ledger            ledger.Ledger // do we need the whole Ledger or just a callback to retrieve account data?
	lastCommittedSlot iotago.SlotIndex

	performanceTracker *performance.Tracker

	errHandler func(error)

	optsInitialCommittee    accounts.AccountsData
	optsSeatManagerProvider module.Provider[*engine.Engine, seatmanager.SeatManager]

	mutex syncutils.Mutex

	module.Module
}

func NewProvider(opts ...options.Option[SybilProtection]) module.Provider[*engine.Engine, sybilprotection.SybilProtection] {
	return module.Provide(func(e *engine.Engine) sybilprotection.SybilProtection {
		s := New(e.NewSubModule("SybilProtection"), e, opts...)

		e.ConstructedEvent().OnTrigger(func() {
			s.ledger = e.Ledger
			s.errHandler = e.ErrorHandler("SybilProtection")
			logger := e.NewChildLogger("PerformanceTracker")
			latestCommittedSlot := e.Storage.Settings().LatestCommitment().Slot()
			latestCommittedEpoch := s.apiProvider.APIForSlot(latestCommittedSlot).TimeProvider().EpochFromSlot(latestCommittedSlot)
			s.performanceTracker = performance.NewTracker(e.Storage.RewardsForEpoch, e.Storage.PoolStats(), e.Storage.Committee(), e.Storage.CommitteeCandidates, e.Storage.ValidatorPerformances, latestCommittedEpoch, e, s.errHandler, logger)
			s.lastCommittedSlot = latestCommittedSlot

			if s.optsInitialCommittee != nil {
				if _, err := s.seatManager.RotateCommittee(0, s.optsInitialCommittee); err != nil {
					panic(ierrors.Wrap(err, "error while registering initial committee for epoch 0"))
				}
			}

			s.ConstructedEvent().Trigger()

			// When the engine is triggered initialized, snapshot has been read or database has been initialized properly,
			// so the committee should be available in the performance manager.
			e.InitializedEvent().OnTrigger(func() {
				// Mark the committee for the last committed slot as active.
				currentEpoch := e.CommittedAPI().TimeProvider().EpochFromSlot(e.Storage.Settings().LatestCommitment().Slot())
				err := s.seatManager.InitializeCommittee(currentEpoch, e.Clock.Accepted().RelativeTime())
				if err != nil {
					panic(ierrors.Wrap(err, "error while initializing committee"))
				}

				s.InitializedEvent().Trigger()
			})

			e.Events.SlotGadget.SlotFinalized.Hook(s.slotFinalized)

			e.Events.SybilProtection.LinkTo(s.events)

			s.InitializedEvent().Trigger()
		})

		return s
	})
}

func New(subModule module.Module, engine *engine.Engine, opts ...options.Option[SybilProtection]) *SybilProtection {
	return options.Apply(&SybilProtection{
		Module:                  subModule,
		events:                  sybilprotection.NewEvents(),
		apiProvider:             engine,
		optsSeatManagerProvider: topstakers.NewProvider(),
	}, opts, func(s *SybilProtection) {
		s.seatManager = s.optsSeatManagerProvider(engine)

		s.ShutdownEvent().OnTrigger(func() {
			s.StoppedEvent().Trigger()
		})
	})
}

func (o *SybilProtection) TrackBlock(block *blocks.Block) {
	if block.IsValidationBlock() {
		o.performanceTracker.TrackValidationBlock(block)

		return
	}

	if block.Payload().PayloadType() != iotago.PayloadCandidacyAnnouncement {
		return
	}

	accountData, exists, err := o.ledger.Account(block.IssuerID(), block.SlotCommitmentID().Slot())
	if err != nil {
		o.errHandler(ierrors.Wrapf(err, "error while retrieving data for account %s in slot %d from accounts ledger", block.IssuerID(), block.SlotCommitmentID().Slot()))

		return
	}

	if !exists {
		return
	}

	blockEpoch := o.apiProvider.APIForSlot(block.ID().Slot()).TimeProvider().EpochFromSlot(block.ID().Slot())

	// if the block is issued before the stake end epoch, then it's not a valid validator or candidate block
	if accountData.StakeEndEpoch() < blockEpoch {
		return
	}

	// if a candidate block is issued in the stake end epoch,
	// or if block is issued after EpochEndSlot - EpochNearingThreshold, because candidates can register only until that point.
	// then don't consider it because the validator can't be part of the committee in the next epoch
	if accountData.StakeEndEpoch() == blockEpoch ||
		block.ID().Slot()+o.apiProvider.APIForSlot(block.ID().Slot()).ProtocolParameters().EpochNearingThreshold() > o.apiProvider.APIForSlot(block.ID().Slot()).TimeProvider().EpochEnd(blockEpoch) {
		return
	}

	o.performanceTracker.TrackCandidateBlock(block)
}

func (o *SybilProtection) CommitSlot(slot iotago.SlotIndex) (committeeRoot iotago.Identifier, rewardsRoot iotago.Identifier, err error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	apiForSlot := o.apiProvider.APIForSlot(slot)
	timeProvider := apiForSlot.TimeProvider()
	currentEpoch := timeProvider.EpochFromSlot(slot)
	nextEpoch := currentEpoch + 1
	prevEpoch := lo.Return1(safemath.SafeSub(currentEpoch, 1))
	currentEpochEndSlot := timeProvider.EpochEnd(currentEpoch)
	isEpochEndSlot := slot == currentEpochEndSlot
	maxCommittableAge := apiForSlot.ProtocolParameters().MaxCommittableAge()

	// Determine the committee root.
	{
		// If the committed slot is `maxCommittableAge` away from the end of the epoch, then register (reuse)
		// a committee for the next epoch if it hasn't been selected yet.
		if slot+maxCommittableAge == currentEpochEndSlot {
			if committee, committeeExists := o.seatManager.CommitteeInEpoch(nextEpoch); !committeeExists {
				// If the committee for the epoch wasn't set before due to finalization of a slot,
				// we promote the current committee to also serve in the next epoch.
				committeeAccounts, err := o.reuseCommittee(currentEpoch, nextEpoch)
				if err != nil {
					return iotago.Identifier{}, iotago.Identifier{}, ierrors.Wrapf(err, "failed to reuse committee for epoch %d", nextEpoch)
				}

				o.events.CommitteeSelected.Trigger(committeeAccounts, nextEpoch)
				o.LogDebug("reusing committee", "nextEpoch", nextEpoch, "committeeIDs", committeeAccounts.IDs())
			} else {
				o.LogDebug("not reusing committee", "nextEpoch", nextEpoch, "committeeIDs", committee.IDs())
			}
		}

		targetCommitteeEpoch := currentEpoch
		if slot+maxCommittableAge >= currentEpochEndSlot {
			targetCommitteeEpoch = nextEpoch
		}

		committeeRoot, err = o.committeeRoot(targetCommitteeEpoch)
		if err != nil {
			return iotago.Identifier{}, iotago.Identifier{}, ierrors.Wrapf(err, "failed to calculate committee root for epoch %d", targetCommitteeEpoch)
		}
	}

	// Handle performance tracking for the current epoch.
	{
		if isEpochEndSlot {
			committee, exists := o.performanceTracker.LoadCommitteeForEpoch(currentEpoch)
			if !exists {
				return iotago.Identifier{}, iotago.Identifier{}, ierrors.Wrapf(err, "committee for a finished epoch %d not found", currentEpoch)
			}

			err = o.performanceTracker.ApplyEpoch(currentEpoch, committee)
			if err != nil {
				return iotago.Identifier{}, iotago.Identifier{}, ierrors.Wrapf(err, "failed to apply epoch %d", currentEpoch)
			}
		}
	}

	// Determine the rewards root.
	{
		// We only update the rewards root if the slot is the last slot of the epoch.
		// Otherwise, we reuse the rewards root from the previous epoch.
		targetRewardsEpoch := prevEpoch
		if isEpochEndSlot {
			targetRewardsEpoch = currentEpoch
		}

		rewardsRoot, err = o.performanceTracker.RewardsRoot(targetRewardsEpoch)
		if err != nil {
			return iotago.Identifier{}, iotago.Identifier{}, ierrors.Wrapf(err, "failed to calculate rewards root for epoch %d", targetRewardsEpoch)
		}
	}

	o.lastCommittedSlot = slot

	return committeeRoot, rewardsRoot, nil
}

func (o *SybilProtection) RewardsRoot(epoch iotago.EpochIndex) (rewardsRoot iotago.Identifier, err error) {
	return o.performanceTracker.RewardsRoot(epoch)
}

func (o *SybilProtection) committeeRoot(targetCommitteeEpoch iotago.EpochIndex) (committeeRoot iotago.Identifier, err error) {
	committee, exists := o.performanceTracker.LoadCommitteeForEpoch(targetCommitteeEpoch)
	if !exists {
		return iotago.Identifier{}, ierrors.Wrapf(err, "committee for an epoch %d not found", targetCommitteeEpoch)
	}

	committeeTree := ads.NewSet[iotago.Identifier](
		mapdb.NewMapDB(),
		iotago.Identifier.Bytes,
		iotago.IdentifierFromBytes,
		iotago.AccountID.Bytes,
		iotago.AccountIDFromBytes,
	)

	committeeIDs := committee.IDs()

	o.LogTrace("generating committee root", "committeeIDs", committeeIDs, "epoch", targetCommitteeEpoch)

	for _, accountID := range committeeIDs {
		if err = committeeTree.Add(accountID); err != nil {
			return iotago.Identifier{}, ierrors.Wrapf(err, "failed to add account %s to committee tree", accountID)
		}
	}

	return committeeTree.Root(), nil
}

func (o *SybilProtection) SeatManager() seatmanager.SeatManager {
	return o.seatManager
}

func (o *SybilProtection) ValidatorReward(validatorID iotago.AccountID, stakingFeature *iotago.StakingFeature, claimingEpoch iotago.EpochIndex) (validatorReward iotago.Mana, firstRewardEpoch iotago.EpochIndex, lastRewardEpoch iotago.EpochIndex, err error) {
	return o.performanceTracker.ValidatorReward(validatorID, stakingFeature, claimingEpoch)
}

func (o *SybilProtection) DelegatorReward(validatorID iotago.AccountID, delegatedAmount iotago.BaseToken, epochStart iotago.EpochIndex, epochEnd iotago.EpochIndex, claimingEpoch iotago.EpochIndex) (delegatorReward iotago.Mana, firstRewardEpoch iotago.EpochIndex, lastRewardEpoch iotago.EpochIndex, err error) {
	return o.performanceTracker.DelegatorReward(validatorID, delegatedAmount, epochStart, epochEnd, claimingEpoch)
}

func (o *SybilProtection) PoolRewardsForAccount(accountID iotago.AccountID) (
	poolRewardsForAccount iotago.Mana,
	exists bool,
	err error,
) {
	return o.performanceTracker.PoolRewardsForAccount(accountID)
}

func (o *SybilProtection) Import(reader io.ReadSeeker) error {
	return o.performanceTracker.Import(reader)
}

func (o *SybilProtection) Export(writer io.WriteSeeker, targetSlot iotago.SlotIndex) error {
	return o.performanceTracker.Export(writer, targetSlot)
}

// Reset resets the component to a clean state as if it was created at the last commitment.
func (o *SybilProtection) Reset() {
	o.performanceTracker.Reset(o.lastCommittedSlot)
}

func (o *SybilProtection) slotFinalized(slot iotago.SlotIndex) {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	apiForSlot := o.apiProvider.APIForSlot(slot)
	timeProvider := apiForSlot.TimeProvider()
	epoch := timeProvider.EpochFromSlot(slot)

	// Only select new committee if the finalized slot is epochEndNearingThreshold slots from EpochEnd and the last
	// committed slot is earlier than (the last slot of the epoch - maxCommittableAge).
	// Otherwise, skip committee selection because it's too late and the committee has been reused.
	epochEndSlot := timeProvider.EpochEnd(epoch)
	if slot+apiForSlot.ProtocolParameters().EpochNearingThreshold() == epochEndSlot &&
		o.lastCommittedSlot < epochEndSlot-apiForSlot.ProtocolParameters().MaxCommittableAge() {
		newCommittee, err := o.selectNewCommittee(slot)
		if err != nil {
			panic(ierrors.Wrap(err, "error while selecting new committee"))
		}
		o.events.CommitteeSelected.Trigger(newCommittee, epoch+1)
	}
}

// IsCandidateActive returns true if the given validator is currently active.
func (o *SybilProtection) IsCandidateActive(validatorID iotago.AccountID, epoch iotago.EpochIndex) (bool, error) {
	activeCandidates, err := o.performanceTracker.EligibleValidatorCandidates(epoch)
	if err != nil {
		return false, ierrors.Wrapf(err, "failed to retrieve eligible candidates")
	}

	return activeCandidates.Has(validatorID), nil
}

// EligibleValidators returns the currently known list of recently active validator candidates for the given epoch.
func (o *SybilProtection) EligibleValidators(epoch iotago.EpochIndex) (accounts.AccountsData, error) {
	candidates, err := o.performanceTracker.EligibleValidatorCandidates(epoch)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to retrieve eligible validator candidates for epoch %d", epoch)
	}

	validators := make(accounts.AccountsData, 0)

	if err = candidates.ForEach(func(candidate iotago.AccountID) error {
		accountData, exists, err := o.ledger.Account(candidate, o.lastCommittedSlot)
		if err != nil {
			return ierrors.Wrapf(err, "failed to load account data for candidate %s", candidate)
		}
		if !exists {
			return ierrors.Errorf("account of committee candidate %s does not exist", candidate)
		}
		// if `End Epoch` is the current one or has passed, validator is no longer considered for validator selection
		if accountData.StakeEndEpoch() <= epoch {
			return nil
		}
		validators = append(validators, accountData.Clone())

		return nil
	}); err != nil {
		return nil, ierrors.Wrapf(err, "failed to iterate over eligible validator candidates")
	}

	return validators, nil
}

// OrderedRegisteredCandidateValidatorsList returns the currently known list of registered validator candidates for the given epoch.
func (o *SybilProtection) OrderedRegisteredCandidateValidatorsList(epoch iotago.EpochIndex) ([]*api.ValidatorResponse, error) {
	candidates, err := o.performanceTracker.ValidatorCandidates(epoch)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to retrieve candidates")
	}

	activeCandidates, err := o.performanceTracker.EligibleValidatorCandidates(epoch)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to retrieve eligible candidates")
	}

	validatorResp := make([]*api.ValidatorResponse, 0, candidates.Size())
	if err := candidates.ForEach(func(candidate iotago.AccountID) error {
		accountData, exists, err := o.ledger.Account(candidate, o.lastCommittedSlot)
		if err != nil {
			return ierrors.Wrapf(err, "failed to get account %s", candidate)
		}
		if !exists {
			return ierrors.Errorf("account of committee candidate %s does not exist", candidate)
		}
		// if `End Epoch` is the current one or has passed, validator is no longer considered for validator selection
		if accountData.StakeEndEpoch() <= epoch {
			return nil
		}
		active := activeCandidates.Has(candidate)
		validatorResp = append(validatorResp, &api.ValidatorResponse{
			AddressBech32:                  accountData.ID().ToAddress().Bech32(o.apiProvider.CommittedAPI().ProtocolParameters().Bech32HRP()),
			StakingEndEpoch:                accountData.StakeEndEpoch(),
			PoolStake:                      accountData.ValidatorStake() + accountData.DelegationStake(),
			ValidatorStake:                 accountData.ValidatorStake(),
			FixedCost:                      accountData.FixedCost(),
			Active:                         active,
			LatestSupportedProtocolVersion: accountData.LatestSupportedProtocolVersionAndHash().Version,
			LatestSupportedProtocolHash:    accountData.LatestSupportedProtocolVersionAndHash().Hash,
		})

		return nil
	}); err != nil {
		return nil, ierrors.Wrapf(err, "failed to iterate over eligible validator candidates")
	}

	// sort validators by pool stake, then by address
	sort.Slice(validatorResp, func(i int, j int) bool {
		if validatorResp[i].PoolStake == validatorResp[j].PoolStake {
			return validatorResp[i].AddressBech32 < validatorResp[j].AddressBech32
		}

		return validatorResp[i].PoolStake > validatorResp[j].PoolStake
	})

	return validatorResp, nil
}

func (o *SybilProtection) reuseCommittee(currentEpoch iotago.EpochIndex, targetEpoch iotago.EpochIndex) (*account.SeatedAccounts, error) {
	committee, err := o.seatManager.ReuseCommittee(currentEpoch, targetEpoch)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to set committee for epoch %d", targetEpoch)
	}

	o.performanceTracker.ClearCandidates()

	return committee, nil
}

func (o *SybilProtection) selectNewCommittee(slot iotago.SlotIndex) (*account.SeatedAccounts, error) {
	timeProvider := o.apiProvider.APIForSlot(slot).TimeProvider()
	currentEpoch := timeProvider.EpochFromSlot(slot)
	nextEpoch := currentEpoch + 1

	// We get the list of candidates for the next epoch. They are registered in the current epoch.
	candidates, err := o.performanceTracker.EligibleValidatorCandidates(currentEpoch)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to retrieve candidates for epoch %d", nextEpoch)
	}

	o.LogDebug("selecting new committee", "candidates", candidates.ToSlice(), "slot", slot)

	// If there's no candidate, reuse the current committee.
	if candidates.Size() == 0 {
		committee, err := o.reuseCommittee(currentEpoch, nextEpoch)
		if err != nil {
			return nil, ierrors.Wrapf(err, "failed to reuse committee (due to no candidates) for epoch %d", nextEpoch)
		}

		o.LogDebug("candidates empty, reusing committee", "candidates", committee.IDs(), "slot", slot)

		return committee, nil
	}

	candidateAccounts := make(accounts.AccountsData, 0)
	if err := candidates.ForEach(func(candidate iotago.AccountID) error {
		accountData, exists, err := o.ledger.Account(candidate, slot)
		if err != nil {
			return err
		}
		if !exists {
			return ierrors.Errorf("account of committee candidate %s does not exist in slot %d", candidate, slot)
		}

		candidateAccounts = append(candidateAccounts, accountData)

		return nil
	}); err != nil {
		return nil, ierrors.Wrap(err, "failed to iterate through candidates")
	}

	newCommittee, err := o.seatManager.RotateCommittee(nextEpoch, candidateAccounts)
	if err != nil {
		return nil, ierrors.Wrap(err, "failed to rotate committee")
	}

	o.performanceTracker.ClearCandidates()

	o.LogDebug("rotating committee", "candidates", newCommittee.IDs(), "slot", slot)

	return newCommittee, nil
}

// WithInitialCommittee registers the passed committee on a given slot.
// This is needed to generate Genesis snapshot with some initial committee.
func WithInitialCommittee(committee accounts.AccountsData) options.Option[SybilProtection] {
	return func(o *SybilProtection) {
		o.optsInitialCommittee = committee
	}
}

func WithSeatManagerProvider(seatManagerProvider module.Provider[*engine.Engine, seatmanager.SeatManager]) options.Option[SybilProtection] {
	return func(o *SybilProtection) {
		o.optsSeatManagerProvider = seatManagerProvider
	}
}
