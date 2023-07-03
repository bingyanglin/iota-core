package performance

import (
	"sync"

	"github.com/cockroachdb/errors"

	"github.com/iotaledger/hive.go/ads"
	"github.com/iotaledger/hive.go/ds/advancedset"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Tracker struct {
	rewardBaseStore kvstore.KVStore
	poolStatsStore  kvstore.KVStore
	committeeStore  kvstore.KVStore

	performanceFactorsFunc func(slot iotago.SlotIndex) *prunable.PerformanceFactors

	timeProvider  *iotago.TimeProvider
	decayProvider *iotago.ManaDecayProvider

	performanceFactorsMutex sync.RWMutex
	mutex                   sync.RWMutex
}

func NewTracker(
	rewardsBaseStore kvstore.KVStore,
	poolStatsStore kvstore.KVStore,
	committeeStore kvstore.KVStore,
	performanceFactorsFunc func(slot iotago.SlotIndex) *prunable.PerformanceFactors,
	timeProvider *iotago.TimeProvider,
	decayProvider *iotago.ManaDecayProvider,
) *Tracker {
	return &Tracker{
		rewardBaseStore:        rewardsBaseStore,
		poolStatsStore:         poolStatsStore,
		committeeStore:         committeeStore,
		performanceFactorsFunc: performanceFactorsFunc,
		timeProvider:           timeProvider,
		decayProvider:          decayProvider,
	}
}

func (t *Tracker) RegisterCommittee(epoch iotago.EpochIndex, committee *account.Accounts) error {
	return t.storeCommitteeForEpoch(epoch, committee)
}

func (t *Tracker) BlockAccepted(block *blocks.Block) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.performanceFactorsMutex.Lock()
	defer t.performanceFactorsMutex.Unlock()

	// TODO: check if this block is a validator block

	performanceFactors := t.performanceFactorsFunc(block.ID().Index())
	pf, err := performanceFactors.Load(block.Block().IssuerID)
	if err != nil {
		panic(err)
	}

	err = performanceFactors.Store(block.Block().IssuerID, pf+1)
	if err != nil {
		panic(err)
	}
}

func (t *Tracker) ApplyEpoch(epoch iotago.EpochIndex, committee *account.Accounts) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	epochSlotStart := t.timeProvider.EpochStart(epoch)
	epochSlotEnd := t.timeProvider.EpochEnd(epoch)

	profitMargin := calculateProfitMargin(committee.TotalValidatorStake(), committee.TotalStake())
	poolsStats := PoolsStats{
		TotalStake:          committee.TotalStake(),
		TotalValidatorStake: committee.TotalValidatorStake(),
		ProfitMargin:        profitMargin,
	}

	if err := t.poolStatsStore.Set(epoch.Bytes(), lo.PanicOnErr(poolsStats.Bytes())); err != nil {
		panic(errors.Wrapf(err, "failed to store pool stats for epoch %d", epoch))
	}

	committee.ForEach(func(accountID iotago.AccountID, pool *account.Pool) bool {
		intermediateFactors := make([]uint64, 0)
		for slot := epochSlotStart; slot <= epochSlotEnd; slot++ {
			performanceFactorStorage := t.performanceFactorsFunc(slot)
			if performanceFactorStorage == nil {
				intermediateFactors = append(intermediateFactors, 0)
			}

			pf, err := performanceFactorStorage.Load(accountID)
			if err != nil {
				panic(errors.Wrapf(err, "failed to load performance factor for account %s", accountID))
			}

			intermediateFactors = append(intermediateFactors, pf)

		}

		ads.NewMap[iotago.AccountID, PoolRewards](t.rewardsStorage(epoch)).Set(accountID, &PoolRewards{
			PoolStake:   pool.PoolStake,
			PoolRewards: t.poolReward(epochSlotEnd, committee.TotalValidatorStake(), committee.TotalStake(), pool.PoolStake, pool.ValidatorStake, pool.FixedCost, t.aggregatePerformanceFactors(intermediateFactors)),
			FixedCost:   pool.FixedCost,
		})

		return true
	})
}

func (t *Tracker) EligibleValidatorCandidates(_ iotago.EpochIndex) *advancedset.AdvancedSet[iotago.AccountID] {
	// TODO: we should choose candidates we tracked performance for

	return &advancedset.AdvancedSet[iotago.AccountID]{}
}

func (t *Tracker) poolStats(epoch iotago.EpochIndex) (poolStats *PoolsStats, err error) {
	poolStats = new(PoolsStats)
	poolStatsBytes, err := t.poolStatsStore.Get(epoch.Bytes())
	if err != nil {
		return poolStats, errors.Wrapf(err, "failed to get pool stats for epoch %d", epoch)
	}

	if _, err := poolStats.FromBytes(poolStatsBytes); err != nil {
		return poolStats, errors.Wrapf(err, "failed to parse pool stats for epoch %d", epoch)
	}

	return poolStats, nil
}

func (t *Tracker) LoadCommitteeForEpoch(epoch iotago.EpochIndex) (committee *account.Accounts, exists bool) {
	accountsBytes, err := t.committeeStore.Get(epoch.Bytes())
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, false
		}
		panic(errors.Wrapf(err, "failed to load committee for epoch %d", epoch))
	}

	committee = account.NewAccounts()
	if _, err = committee.FromBytes(accountsBytes); err != nil {
		panic(errors.Wrapf(err, "failed to parse committee for epoch %d", epoch))
	}

	return committee, true
}

func (t *Tracker) storeCommitteeForEpoch(epochIndex iotago.EpochIndex, committee *account.Accounts) error {
	committeeBytes, err := committee.Bytes()
	if err != nil {
		return err
	}

	return t.committeeStore.Set(epochIndex.Bytes(), committeeBytes)
}

func (t *Tracker) aggregatePerformanceFactors(issuedBlocksPerSlot []uint64) uint64 {
	var sum uint64
	for _, issuedBlocks := range issuedBlocksPerSlot {
		if issuedBlocks > uint64(validatorBlocksPerSlot) {
			// we harshly punish validators that issue any blocks more than allowed
			return 0
		}
		sum += issuedBlocks
	}

	return sum / uint64(len(issuedBlocksPerSlot))
}
