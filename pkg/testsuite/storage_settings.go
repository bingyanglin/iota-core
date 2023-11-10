package testsuite

import (
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (t *TestSuite) AssertSnapshotImported(imported bool, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if imported != node.Protocol.MainEngineInstance().Storage.Settings().IsSnapshotImported() {
				return ierrors.Errorf("AssertSnapshotImported: %s: expected %v, got %v", node.Name, imported, node.Protocol.MainEngineInstance().Storage.Settings().IsSnapshotImported())
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertProtocolParameters(parameters iotago.ProtocolParameters, nodes ...*mock.Node) {
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

func (t *TestSuite) AssertLatestCommitment(commitment *iotago.Commitment, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if !commitment.Equals(node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().Commitment()) {
				return ierrors.Errorf("AssertLatestCommitment: %s: expected %s, got %s", node.Name, commitment, node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment())
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertCommitmentSlotIndexExists(slot iotago.SlotIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().ID().Slot() < slot {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: commitment with at least %v not found in settings.LatestCommitment()", node.Name, slot)
			}

			cm, err := node.Protocol.MainEngineInstance().Storage.Commitments().Load(slot)
			if err != nil {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: expected %v, got error %v", node.Name, slot, err)
			}

			if cm == nil {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: commitment at index %v not found", node.Name, slot)
			}

			// Make sure the commitment is also available in the ChainManager.
			if node.Protocol.ChainManager.RootCommitment().Chain().LatestCommitment().ID().Slot() < slot {
				return ierrors.Errorf("AssertCommitmentSlotIndexExists: %s: commitment at index %v not found in ChainManager", node.Name, slot)
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertLatestCommitmentSlotIndex(slot iotago.SlotIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if slot != node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().Slot() {
				return ierrors.Errorf("AssertLatestCommitmentSlotIndex: %s: expected %v, got %v", node.Name, slot, node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().Slot())
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertLatestCommitmentCumulativeWeight(cw uint64, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if cw != node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().CumulativeWeight() {
				return ierrors.Errorf("AssertLatestCommitmentCumulativeWeight: %s: expected %v, got %v", node.Name, cw, node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().CumulativeWeight())
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertLatestFinalizedSlot(slot iotago.SlotIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			if slot != node.Protocol.MainEngineInstance().Storage.Settings().LatestFinalizedSlot() {
				return ierrors.Errorf("AssertLatestFinalizedSlot: %s: expected %d, got %d from settings", node.Name, slot, node.Protocol.MainEngineInstance().Storage.Settings().LatestFinalizedSlot())
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertChainID(expectedChainID iotago.CommitmentID, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			actualChainID := node.Protocol.MainEngineInstance().ChainID()
			if expectedChainID != node.Protocol.MainEngineInstance().ChainID() {
				return ierrors.Errorf("AssertChainID: %s: expected %s (index: %d), got %s (index: %d)", node.Name, expectedChainID, expectedChainID.Slot(), actualChainID, actualChainID.Slot())
			}

			return nil
		})
	}
}
