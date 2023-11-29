package protocol

import (
	"time"

	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol/chainmanager"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/attestation"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blockdag"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/booker"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/clock"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/blockgadget"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/consensus/slotgadget"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter/postsolidfilter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter/presolidfilter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/ledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/syncmanager"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/tipmanager"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/tipselection"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/upgrade"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection"
	"github.com/iotaledger/iota-core/pkg/storage"
)

func WithBaseDirectory(baseDirectory string) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsBaseDirectory = baseDirectory
	}
}

func WithSnapshotPath(snapshot string) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsSnapshotPath = snapshot
	}
}

func WithChainSwitchingThreshold(threshold int) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsChainSwitchingThreshold = threshold
	}
}

func WithPreSolidFilterProvider(optsPreSolidFilterProvider module.Provider[*engine.Engine, presolidfilter.PreSolidFilter]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsPreSolidFilterProvider = optsPreSolidFilterProvider
	}
}

func WithPostSolidFilterProvider(optsPostSolidFilterProvider module.Provider[*engine.Engine, postsolidfilter.PostSolidFilter]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsPostSolidFilterProvider = optsPostSolidFilterProvider
	}
}

func WithBlockDAGProvider(optsBlockDAGProvider module.Provider[*engine.Engine, blockdag.BlockDAG]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsBlockDAGProvider = optsBlockDAGProvider
	}
}

func WithTipManagerProvider(optsTipManagerProvider module.Provider[*engine.Engine, tipmanager.TipManager]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsTipManagerProvider = optsTipManagerProvider
	}
}

func WithTipSelectionProvider(optsTipSelectionProvider module.Provider[*engine.Engine, tipselection.TipSelection]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsTipSelectionProvider = optsTipSelectionProvider
	}
}

func WithBookerProvider(optsBookerProvider module.Provider[*engine.Engine, booker.Booker]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsBookerProvider = optsBookerProvider
	}
}

func WithClockProvider(optsClockProvider module.Provider[*engine.Engine, clock.Clock]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsClockProvider = optsClockProvider
	}
}

func WithSybilProtectionProvider(optsSybilProtectionProvider module.Provider[*engine.Engine, sybilprotection.SybilProtection]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsSybilProtectionProvider = optsSybilProtectionProvider
	}
}

func WithBlockGadgetProvider(optsBlockGadgetProvider module.Provider[*engine.Engine, blockgadget.Gadget]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsBlockGadgetProvider = optsBlockGadgetProvider
	}
}

func WithSlotGadgetProvider(optsSlotGadgetProvider module.Provider[*engine.Engine, slotgadget.Gadget]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsSlotGadgetProvider = optsSlotGadgetProvider
	}
}

func WithEpochGadgetProvider(optsEpochGadgetProvider module.Provider[*engine.Engine, sybilprotection.SybilProtection]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsSybilProtectionProvider = optsEpochGadgetProvider
	}
}

func WithNotarizationProvider(optsNotarizationProvider module.Provider[*engine.Engine, notarization.Notarization]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsNotarizationProvider = optsNotarizationProvider
	}
}

func WithAttestationProvider(optsAttestationProvider module.Provider[*engine.Engine, attestation.Attestations]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsAttestationProvider = optsAttestationProvider
	}
}

func WithLedgerProvider(optsLedgerProvider module.Provider[*engine.Engine, ledger.Ledger]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsLedgerProvider = optsLedgerProvider
	}
}

func WithUpgradeOrchestratorProvider(optsUpgradeOrchestratorProvider module.Provider[*engine.Engine, upgrade.Orchestrator]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsUpgradeOrchestratorProvider = optsUpgradeOrchestratorProvider
	}
}

func WithSyncManagerProvider(optsSyncManagerProvider module.Provider[*engine.Engine, syncmanager.SyncManager]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsSyncManagerProvider = optsSyncManagerProvider
	}
}

func WithEngineOptions(opts ...options.Option[engine.Engine]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsEngineOptions = append(p.optsEngineOptions, opts...)
	}
}

func WithChainManagerOptions(opts ...options.Option[chainmanager.Manager]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsChainManagerOptions = append(p.optsChainManagerOptions, opts...)
	}
}

func WithStorageOptions(opts ...options.Option[storage.Storage]) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsStorageOptions = append(p.optsStorageOptions, opts...)
	}
}

func WithAttestationRequesterTryInterval(t time.Duration) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsAttestationRequesterTryInterval = t
	}
}

func WithAttestationRequesterMaxTries(n int) options.Option[Protocol] {
	return func(p *Protocol) {
		p.optsAttestationRequesterMaxRetries = n
	}
}
