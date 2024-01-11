package core

import (
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (t *Simulator) AssertSnapshotImported(imported bool, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if imported != node.Protocol.Engines.Main.Get().Storage.Settings().IsSnapshotImported() {
				return ierrors.Errorf("AssertSnapshotImported: %s: expected %v, got %v", node.Name, imported, node.Protocol.Engines.Main.Get().Storage.Settings().IsSnapshotImported())
			}

			return nil
		})
	}
}

func (t *Simulator) AssertProtocolParameters(parameters iotago.ProtocolParameters, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if !parameters.Equals(node.Protocol.CommittedAPI().ProtocolParameters()) {
				return ierrors.Errorf("AssertProtocolParameters: %s: expected %s, got %s", node.Name, parameters, node.Protocol.CommittedAPI().ProtocolParameters())
			}

			return nil
		})
	}
}

func (t *Simulator) AssertLatestCommitment(commitment *iotago.Commitment, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if !commitment.Equals(node.Protocol.Engines.Main.Get().Storage.Settings().LatestCommitment().Commitment()) {
				return ierrors.Errorf("AssertLatestCommitment: %s: expected %s, got %s", node.Name, commitment, node.Protocol.Engines.Main.Get().Storage.Settings().LatestCommitment().Commitment())
			}

			return nil
		})
	}
}

func (t *Simulator) AssertCommitmentSlotIndexExists(slot iotago.SlotIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if node.Protocol.Engines.Main.Get().Storage.Settings().LatestCommitment().ID().Slot() < slot {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: commitment with at least %v not found in settings.LatestCommitment()", node.Name, slot)
			}

			cm, err := node.Protocol.Engines.Main.Get().Storage.Commitments().Load(slot)
			if err != nil {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: expected %v, got error %v", node.Name, slot, err)
			}

			if cm == nil {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: commitment at index %v not found", node.Name, slot)
			}

			// Make sure the commitment is also available in the ChainManager.
			if node.Protocol.Chains.Main.Get().LatestCommitment.Get().ID().Slot() < slot {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: commitment at index %v not found in ChainManager", node.Name, slot)
			}

			return nil
		})
	}
}

func (t *Simulator) AssertLatestCommitmentSlotIndex(slot iotago.SlotIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			latestCommittedSlot := node.Protocol.Engines.Main.Get().Storage.Settings().LatestCommitment().Slot()
			if slot != latestCommittedSlot {
				return ierrors.Errorf("AssertLatestCommitmentSlotIndex: %s: expected %v, got %v", node.Name, slot, latestCommittedSlot)
			}

			return nil
		})
	}
}

func (t *Simulator) AssertLatestCommitmentCumulativeWeight(cw uint64, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if cw != node.Protocol.Engines.Main.Get().Storage.Settings().LatestCommitment().CumulativeWeight() {
				return ierrors.Errorf("AssertLatestCommitmentCumulativeWeight: %s: expected %v, got %v", node.Name, cw, node.Protocol.Engines.Main.Get().Storage.Settings().LatestCommitment().CumulativeWeight())
			}

			return nil
		})
	}
}

func (t *Simulator) AssertLatestFinalizedSlot(slot iotago.SlotIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if slot != node.Protocol.Engines.Main.Get().Storage.Settings().LatestFinalizedSlot() {
				return ierrors.Errorf("AssertLatestFinalizedSlot: %s: expected %d, got %d from settings", node.Name, slot, node.Protocol.Engines.Main.Get().Storage.Settings().LatestFinalizedSlot())
			}

			return nil
		})
	}
}

func (t *Simulator) AssertChainID(expectedChainID iotago.CommitmentID, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			actualChainID := node.Protocol.Chains.Main.Get().ForkingPoint.Get().ID()

			if expectedChainID != actualChainID {
				return ierrors.Errorf("AssertChainID: %s: expected %s (index: %d), got %s (index: %d)", node.Name, expectedChainID, expectedChainID.Slot(), actualChainID, actualChainID.Slot())
			}

			return nil
		})
	}
}
