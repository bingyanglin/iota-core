package performance

import (
	"fmt"

	"github.com/iotaledger/hive.go/ads"
	"github.com/iotaledger/hive.go/core/safemath"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/stringify"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (t *Tracker) RewardsRoot(epoch iotago.EpochIndex) (iotago.Identifier, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	m, err := t.rewardsMap(epoch)
	if err != nil {
		return iotago.Identifier{}, err
	}

	root := m.Root()

	builder := stringify.NewStructBuilder("RewardsRoot")
	builder.AddField(stringify.NewStructField("Root", root))
	builder.AddField(stringify.NewStructField("WasRestoredFromStorage", m.WasRestoredFromStorage()))

	if err := m.Stream(func(accountID iotago.AccountID, poolRewards *model.PoolRewards) error {
		builder.AddField(stringify.NewStructField(fmt.Sprintf("account[%s]", accountID.String()), poolRewards))

		return nil
	}); err != nil {
		panic(err)
	}

	t.LogDebug("RewardsRoot", "epoch", epoch, "rewardsMap", builder)

	return root, nil
}

func (t *Tracker) ValidatorReward(validatorID iotago.AccountID, stakingFeature *iotago.StakingFeature, claimingEpoch iotago.EpochIndex) (validatorReward iotago.Mana, firstRewardEpoch iotago.EpochIndex, lastRewardEpoch iotago.EpochIndex, err error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	validatorReward = 0
	stakedAmount := stakingFeature.StakedAmount
	firstRewardEpoch = stakingFeature.StartEpoch
	// Start Epoch = 0 is unmodified as a special case for the initial validators that bootstrap the network to get their rewards.
	// Otherwise, the earliest rewards can be in the epoch for which a validator could have been selected, which is start epoch + 1.
	if firstRewardEpoch != 0 {
		firstRewardEpoch = stakingFeature.StartEpoch + 1
	}
	lastRewardEpoch = stakingFeature.EndEpoch

	// Limit reward fetching only to committed epochs.
	if lastRewardEpoch > t.latestAppliedEpoch {
		lastRewardEpoch = t.latestAppliedEpoch
	}

	decayEndEpoch := t.decayEndEpoch(claimingEpoch, lastRewardEpoch)

	// Only fetch unexpired rewards from epochs by determining the earliest epoch
	// for which rewards are still retained or available.
	firstRewardEpoch = t.earliestRewardEpoch(firstRewardEpoch, claimingEpoch)

	for epoch := firstRewardEpoch; epoch <= lastRewardEpoch; epoch++ {
		rewardsForAccountInEpoch, exists, err := t.rewardsForAccount(validatorID, epoch)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to get rewards for account %s in epoch %d", validatorID, epoch)
		}

		if !exists || rewardsForAccountInEpoch.PoolStake == 0 {
			// updating epoch start for beginning epochs without the reward
			if epoch < lastRewardEpoch && firstRewardEpoch == epoch {
				firstRewardEpoch = epoch + 1
			}

			continue
		}

		poolStats, err := t.poolStatsStore.Load(epoch)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to get pool stats for epoch %d and validator accountID %s", epoch, validatorID)
		}

		if poolStats == nil {
			return 0, 0, 0, ierrors.Errorf("pool stats for epoch %d and validator accountID %s are nil", epoch, validatorID)
		}

		// If a validator's fixed cost is greater than the earned reward, all rewards go to the delegators.
		if rewardsForAccountInEpoch.PoolRewards < rewardsForAccountInEpoch.FixedCost {
			continue
		}
		poolRewardsNoFixedCost := rewardsForAccountInEpoch.PoolRewards - rewardsForAccountInEpoch.FixedCost

		profitMarginExponent := t.apiProvider.APIForEpoch(epoch).ProtocolParameters().RewardsParameters().ProfitMarginExponent
		profitMarginComplement, err := scaleUpComplement(poolStats.ProfitMargin, profitMarginExponent)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate profit margin factor due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		result, err := safemath.SafeMul(poolStats.ProfitMargin, uint64(poolRewardsNoFixedCost))
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate profit margin factor due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		profitMarginFactor := result >> profitMarginExponent

		result, err = safemath.SafeMul(profitMarginComplement, uint64(poolRewardsNoFixedCost))
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate profit margin factor due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		residualValidatorFactor, err := safemath.Safe64MulDiv(result>>profitMarginExponent, uint64(stakedAmount), uint64(rewardsForAccountInEpoch.PoolStake))
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate residual validator factor due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		result, err = safemath.SafeAdd(uint64(rewardsForAccountInEpoch.FixedCost), profitMarginFactor)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate un-decayed epoch reward due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		undecayedEpochRewards, err := safemath.SafeAdd(result, residualValidatorFactor)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate un-decayed epoch rewards due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		decayProvider := t.apiProvider.APIForEpoch(epoch).ManaDecayProvider()
		decayedEpochRewards, err := decayProvider.DecayManaByEpochs(iotago.Mana(undecayedEpochRewards), epoch, decayEndEpoch)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate rewards with decay for epoch %d and validator accountID %s", epoch, validatorID)
		}

		validatorReward, err = safemath.SafeAdd(validatorReward, decayedEpochRewards)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate validator reward due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}
	}

	return validatorReward, firstRewardEpoch, lastRewardEpoch, nil
}

func (t *Tracker) DelegatorReward(validatorID iotago.AccountID, delegatedAmount iotago.BaseToken, epochStart iotago.EpochIndex, epochEnd iotago.EpochIndex, claimingEpoch iotago.EpochIndex) (delegatorReward iotago.Mana, firstRewardEpoch iotago.EpochIndex, lastRewardEpoch iotago.EpochIndex, err error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	var delegatorsReward iotago.Mana

	firstRewardEpoch = epochStart
	lastRewardEpoch = epochEnd

	// limit looping to committed epochs
	if lastRewardEpoch > t.latestAppliedEpoch {
		lastRewardEpoch = t.latestAppliedEpoch
	}

	decayEndEpoch := t.decayEndEpoch(claimingEpoch, lastRewardEpoch)

	// Only fetch unexpired rewards from epochs by determining the earliest epoch
	// for which rewards are still retained or available.
	firstRewardEpoch = t.earliestRewardEpoch(firstRewardEpoch, claimingEpoch)

	for epoch := firstRewardEpoch; epoch <= lastRewardEpoch; epoch++ {
		rewardsForAccountInEpoch, exists, err := t.rewardsForAccount(validatorID, epoch)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to get rewards for account %s in epoch %d", validatorID, epoch)
		}

		if !exists || rewardsForAccountInEpoch.PoolStake == 0 {
			// updating epoch start for beginning epochs without the reward
			if firstRewardEpoch == epoch {
				firstRewardEpoch = epoch + 1
			}

			continue
		}

		poolStats, err := t.poolStatsStore.Load(epoch)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to get pool stats for epoch %d and validator account ID %s", epoch, validatorID)
		}
		if poolStats == nil {
			return 0, 0, 0, ierrors.Errorf("pool stats for epoch %d and validator accountID %s are nil", epoch, validatorID)
		}

		profitMarginExponent := t.apiProvider.APIForEpoch(epoch).ProtocolParameters().RewardsParameters().ProfitMarginExponent
		profitMarginComplement, err := scaleUpComplement(poolStats.ProfitMargin, profitMarginExponent)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate profit margin factor due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		// if pool reward was lower than fixed cost, the whole reward goes to delegators
		poolReward := rewardsForAccountInEpoch.PoolRewards
		// fixed cost was not too high, no punishment for the validator
		if rewardsForAccountInEpoch.PoolRewards >= rewardsForAccountInEpoch.FixedCost {
			poolReward = rewardsForAccountInEpoch.PoolRewards - rewardsForAccountInEpoch.FixedCost
		}

		result, err := safemath.SafeMul(profitMarginComplement, uint64(poolReward))
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to multiply profitMarginComplement and poolReward for unDecayedEpochRewards due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		undecayedEpochRewards, err := safemath.Safe64MulDiv(result>>profitMarginExponent, uint64(delegatedAmount), uint64(rewardsForAccountInEpoch.PoolStake))
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate unDecayedEpochRewards due to overflow for epoch %d and validator accountID %s", epoch, validatorID)
		}

		decayProvider := t.apiProvider.APIForEpoch(epoch).ManaDecayProvider()
		decayedEpochRewards, err := decayProvider.DecayManaByEpochs(iotago.Mana(undecayedEpochRewards), epoch, decayEndEpoch)
		if err != nil {
			return 0, 0, 0, ierrors.Wrapf(err, "failed to calculate rewards with decay for epoch %d and validator accountID %s", epoch, validatorID)
		}

		delegatorsReward += decayedEpochRewards
	}

	return delegatorsReward, firstRewardEpoch, lastRewardEpoch, nil
}

func (t *Tracker) PoolRewardsForAccount(accountID iotago.AccountID) (
	poolRewardsForAccount iotago.Mana,
	exists bool,
	err error,
) {
	rewards, exists, err := t.rewardsForAccount(accountID, t.latestAppliedEpoch)
	if err != nil || !exists {
		return 0, exists, err
	}

	return rewards.PoolRewards, exists, err
}

// Returns the epoch until which rewards are decayed.
//
// When claiming rewards in epoch X for epoch X-1, decay of X-(X-1) = 1 would be applied. Since epoch X is the
// very first epoch in which one can claim those rewards, decaying by 1 is odd, as one could never claim the full reward then.
// Hence, one epoch worth of decay is deducted in general.
//
// The decay end epoch must however be greater or equal than the last epoch for which rewards are claimed, otherwise
// the decay operation would fail since the amount of epochs to decay would be negative.
// Hence, the smallest returned decay end epoch will be the lastRewardEpoch.
func (t *Tracker) decayEndEpoch(claimingEpoch iotago.EpochIndex, lastRewardEpoch iotago.EpochIndex) iotago.EpochIndex {
	if claimingEpoch >= 1 {
		claimingEpoch--
	}

	return lo.Max(claimingEpoch, lastRewardEpoch)
}

// Returns the earliest epoch for which rewards are retained or available, whichever is later.
func (t *Tracker) earliestRewardEpoch(firstRewardEpoch iotago.EpochIndex, claimingEpoch iotago.EpochIndex) iotago.EpochIndex {
	retentionPeriod := iotago.EpochIndex(t.apiProvider.APIForEpoch(claimingEpoch).ProtocolParameters().RewardsParameters().RetentionPeriod)
	var earliestRetainedRewardEpoch iotago.EpochIndex
	if retentionPeriod < claimingEpoch {
		earliestRetainedRewardEpoch = claimingEpoch - retentionPeriod
	}

	return lo.Max(earliestRetainedRewardEpoch, firstRewardEpoch)
}

func (t *Tracker) rewardsMap(epoch iotago.EpochIndex) (ads.Map[iotago.Identifier, iotago.AccountID, *model.PoolRewards], error) {
	kv, err := t.rewardsStorePerEpochFunc(epoch)
	if err != nil {
		return nil, ierrors.Wrapf(err, "failed to get rewards store for epoch %d", epoch)
	}

	return ads.NewMap[iotago.Identifier](kv,
		iotago.Identifier.Bytes,
		iotago.IdentifierFromBytes,
		iotago.AccountID.Bytes,
		iotago.AccountIDFromBytes,
		(*model.PoolRewards).Bytes,
		model.PoolRewardsFromBytes,
	), nil
}

func (t *Tracker) rewardsForAccount(accountID iotago.AccountID, epoch iotago.EpochIndex) (rewardsForAccount *model.PoolRewards, exists bool, err error) {
	m, err := t.rewardsMap(epoch)
	if err != nil {
		return nil, false, err
	}

	return m.Get(accountID)
}

func (t *Tracker) poolReward(slot iotago.SlotIndex, totalValidatorsStake iotago.BaseToken, totalStake iotago.BaseToken, poolStake iotago.BaseToken, validatorStake iotago.BaseToken, performanceFactor uint64) (iotago.Mana, error) {
	apiForSlot := t.apiProvider.APIForSlot(slot)
	epoch := apiForSlot.TimeProvider().EpochFromSlot(slot)
	params := apiForSlot.ProtocolParameters()

	targetReward, err := params.RewardsParameters().TargetReward(epoch, apiForSlot)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed to calculate target reward for slot %d", slot)
	}

	// Notice that, since both pool stake  and validator stake use at most 53 bits of the variable,
	// to not overflow the calculation, PoolCoefficientExponent must be at most 11. Pool Coefficient will then use at most PoolCoefficientExponent + 1 bits.
	poolCoefficient, err := t.calculatePoolCoefficient(poolStake, totalStake, validatorStake, totalValidatorsStake, slot)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed to calculate pool coefficient for slot %d", slot)
	}

	// Since `Pool Coefficient` uses at most 12 bits and `Target Reward(n)` uses at most 50 bits
	// this multiplication will not overflow using uint64 variables.
	result, err := safemath.SafeMul(poolCoefficient, uint64(targetReward))
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed to calculate pool scaled reward due to overflow for slot %d", slot)
	}

	result >>= params.RewardsParameters().PoolCoefficientExponent

	// Since the result above uses at most 50 bits and `Performance Factor` uses at most 8 bits,
	// this multiplication will not overflow using uint64 variables.
	scaledPoolReward, err := safemath.SafeMul(result, performanceFactor)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed to calculate pool reward without fixed costs due to overflow for slot %d", slot)
	}

	result, err = safemath.SafeDiv(scaledPoolReward, uint64(params.ValidationBlocksPerSlot()))
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed to calculate result reward due division by zero for slot %d", slot)
	}
	poolRewardFixedCost := iotago.Mana(result >> 1)

	return poolRewardFixedCost, nil
}

func (t *Tracker) calculatePoolCoefficient(poolStake iotago.BaseToken, totalStake iotago.BaseToken, validatorStake iotago.BaseToken, totalValidatorStake iotago.BaseToken, slot iotago.SlotIndex) (uint64, error) {
	poolCoeffExponent := t.apiProvider.APIForSlot(slot).ProtocolParameters().RewardsParameters().PoolCoefficientExponent
	scaledUpPoolStake, err := safemath.SafeLeftShift(poolStake, poolCoeffExponent)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed in step 1 of pool coefficient calculation due to overflow for slot %d", slot)
	}

	result1, err := safemath.SafeDiv(scaledUpPoolStake, totalStake)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed in step 2 of pool coefficient calculation due to overflow for slot %d", slot)
	}

	scaledUpValidatorStake, err := safemath.SafeLeftShift(validatorStake, poolCoeffExponent)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed in step 3 of pool coefficient calculation due to overflow for slot %d", slot)
	}

	result2, err := safemath.SafeDiv(scaledUpValidatorStake, totalValidatorStake)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed in step 4 of pool coefficient calculation due to overflow for slot %d", slot)
	}

	poolCoeff, err := safemath.SafeAdd(result1, result2)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed in step 5 of pool coefficient calculation due to overflow for slot %d", slot)
	}

	return uint64(poolCoeff), nil
}

// calculateProfitMargin calculates a common profit margin for all validators by firstly increasing the accuracy of the given value, so the profit margin is moved to the power of 2^accuracyShift.
func (t *Tracker) calculateProfitMargin(totalValidatorsStake iotago.BaseToken, totalPoolStake iotago.BaseToken, epoch iotago.EpochIndex) (uint64, error) {
	scaledUpTotalValidatorStake, err := safemath.SafeLeftShift(totalValidatorsStake, t.apiProvider.APIForEpoch(epoch).ProtocolParameters().RewardsParameters().ProfitMarginExponent)
	if err != nil {
		return 0, ierrors.Wrapf(err, "failed to calculate profit margin due to overflow for epoch %d", epoch)
	}

	return uint64(scaledUpTotalValidatorStake / (totalValidatorsStake + totalPoolStake)), nil
}

// scaleUpComplement returns the 'shifted' completition to "one" for the shifted value where one is the 2^accuracyShift.
func scaleUpComplement[V iotago.BaseToken | iotago.Mana | uint64](val V, shift uint8) (V, error) {
	shiftedOne, err := safemath.SafeLeftShift(V(1), shift)
	if err != nil {
		return 0, err
	}

	return safemath.SafeSub(shiftedOne, val)
}
