package prunable

import (
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/ioutils"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/storage/database"
	"github.com/iotaledger/iota-core/pkg/storage/prunable/epochstore"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

type Prunable struct {
	defaultPruningDelay iotago.EpochIndex
	apiProvider         api.Provider
	dbConfig            database.Config
	manager             *Manager
	errorHandler        func(error)

	semiPermanentDB       *database.DBInstance
	decidedUpgradeSignals *epochstore.Store[model.VersionAndHash]
	poolRewards           *epochstore.EpochKVStore
	poolStats             *epochstore.Store[*model.PoolsStats]
	committee             *epochstore.Store[*account.Accounts]
}

func New(dbConfig database.Config, pruningDelay iotago.EpochIndex, apiProvider api.Provider, errorHandler func(error), opts ...options.Option[Manager]) *Prunable {
	semiPermanentDB := database.NewDBInstance(dbConfig)

	return &Prunable{
		dbConfig:            dbConfig,
		defaultPruningDelay: pruningDelay,
		apiProvider:         apiProvider,
		errorHandler:        errorHandler,
		manager:             NewManager(dbConfig, errorHandler, opts...),

		semiPermanentDB:       semiPermanentDB,
		decidedUpgradeSignals: epochstore.NewStore(kvstore.Realm{epochPrefixDecidedUpgradeSignals}, semiPermanentDB.KVStore(), pruningDelayDecidedUpgradeSignals, model.VersionAndHash.Bytes, model.VersionAndHashFromBytes),
		poolRewards:           epochstore.NewEpochKVStore(kvstore.Realm{epochPrefixPoolRewards}, semiPermanentDB.KVStore(), pruningDelayPoolRewards),
		poolStats:             epochstore.NewStore(kvstore.Realm{epochPrefixPoolStats}, semiPermanentDB.KVStore(), pruningDelayPoolStats, (*model.PoolsStats).Bytes, model.PoolsStatsFromBytes),
		committee:             epochstore.NewStore(kvstore.Realm{epochPrefixCommittee}, semiPermanentDB.KVStore(), pruningDelayCommittee, (*account.Accounts).Bytes, account.AccountsFromBytes),
	}
}

func (p *Prunable) RestoreFromDisk() {
	p.manager.RestoreFromDisk()
}

// PruneUntilSlot prunes storage slots less than and equal to the given index.
func (p *Prunable) PruneUntilSlot(index iotago.SlotIndex) {
	epoch := p.apiProvider.CurrentAPI().TimeProvider().EpochFromSlot(index)
	if epoch < p.defaultPruningDelay {
		return
	}

	// prune prunable_epoch
	start := lo.Return1(p.manager.LastPrunedEpoch()) + 1
	for currentIndex := start; currentIndex <= epoch; currentIndex++ {
		p.decidedUpgradeSignals.Prune(currentIndex)
		p.poolRewards.Prune(currentIndex)
		p.poolStats.Prune(currentIndex)
		p.committee.Prune(currentIndex)
	}

	// prune prunable_slot
	p.manager.PruneUntilEpoch(epoch - p.defaultPruningDelay)
}

func (p *Prunable) Size() int64 {
	semiSize, err := ioutils.FolderSize(p.dbConfig.Directory)
	if err != nil {
		p.errorHandler(ierrors.Wrapf(err, "get semiPermanentDB failed for %s", p.dbConfig.Directory))
	}

	return p.manager.PrunableStorageSize() + semiSize
}

func (p *Prunable) Shutdown() {
	p.manager.Shutdown()
	p.semiPermanentDB.Close()
}

func (p *Prunable) LastPrunedEpoch() (index iotago.EpochIndex, hasPruned bool) {
	return p.manager.LastPrunedEpoch()
}
