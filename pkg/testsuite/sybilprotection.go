package testsuite

import (
	"github.com/stretchr/testify/assert"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/accounts"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

func (t *TestSuite) AssertSybilProtectionCommittee(epoch iotago.EpochIndex, expectedAccounts []iotago.AccountID, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			committeeInEpoch, exists := node.Protocol.Engines.Main.Get().SybilProtection.SeatManager().CommitteeInEpoch(epoch)
			if !exists {
				return ierrors.Errorf("AssertSybilProtectionCommittee: %s: failed to get committee in epoch %d", node.Name, epoch)
			}

			committeeInEpochAccounts, err := committeeInEpoch.Accounts()
			if err != nil {
				return ierrors.Errorf("AssertSybilProtectionCommittee: %s: failed to get accounts in committee in epoch %d: %w", node.Name, epoch, err)
			}

			accountIDs := committeeInEpochAccounts.IDs()
			if !assert.ElementsMatch(t.fakeTesting, expectedAccounts, accountIDs) {
				return ierrors.Errorf("AssertSybilProtectionCommittee: %s: expected %s, got %s", node.Name, expectedAccounts, accountIDs)
			}

			if len(expectedAccounts) != len(accountIDs) {
				return ierrors.Errorf("AssertSybilProtectionCommittee: %s: expected %v, got %v", node.Name, len(expectedAccounts), len(accountIDs))
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertReelectedCommitteeSeatIndices(prevEpoch iotago.EpochIndex, newEpoch iotago.EpochIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			committeeInPrevEpoch, exists := node.Protocol.Engines.Main.Get().SybilProtection.SeatManager().CommitteeInEpoch(prevEpoch)
			if !exists {
				return ierrors.Errorf("AssertReelectedCommitteeSeatIndices: %s: failed to get committee in previous epoch %d", node.Name, prevEpoch)
			}

			committeeInNewEpoch, exists := node.Protocol.Engines.Main.Get().SybilProtection.SeatManager().CommitteeInEpoch(newEpoch)
			if !exists {
				return ierrors.Errorf("AssertReelectedCommitteeSeatIndices: %s: failed to get committee in new epoch %d", node.Name, newEpoch)
			}

			committeeInPrevEpochAccounts, err := committeeInPrevEpoch.Accounts()
			if err != nil {
				return ierrors.Errorf("AssertReelectedCommitteeSeatIndices: %s: failed to get accounts in committee in previous epoch %d: %w", node.Name, prevEpoch, err)
			}

			var innerErr error
			committeeInPrevEpochAccounts.ForEach(func(id iotago.AccountID, _ *account.Pool) bool {
				if seatIndex, memberExists := committeeInNewEpoch.GetSeat(id); memberExists {
					if seatIndex != lo.Return1(committeeInPrevEpoch.GetSeat(id)) {
						innerErr = ierrors.Errorf("account %s must have the same SeatIndex in the previous and new epoch committee", id)

						return false
					}
				}

				return true
			})

			return innerErr
		})
	}
}

func (t *TestSuite) AssertSybilProtectionRegisteredValidators(epoch iotago.EpochIndex, expectedAccounts []string, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			candidates, err := node.Protocol.Engines.Main.Get().SybilProtection.OrderedRegisteredCandidateValidatorsList(epoch)
			candidateIDs := lo.Map(candidates, func(candidate *api.ValidatorResponse) string {
				return candidate.AddressBech32
			})
			if err != nil {
				return ierrors.Wrapf(err, "AssertSybilProtectionRegisteredValidators: %s: failed to get registered validators in epoch %d", node.Name, epoch)
			}

			if !assert.ElementsMatch(t.fakeTesting, expectedAccounts, candidateIDs) {
				return ierrors.Errorf("AssertSybilProtectionRegisteredValidators: %s: expected %s, got %s", node.Name, expectedAccounts, candidateIDs)
			}

			if len(expectedAccounts) != len(candidates) {
				return ierrors.Errorf("AssertSybilProtectionRegisteredValidators: %s: expected %v, got %v", node.Name, len(expectedAccounts), len(candidateIDs))
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertSybilProtectionCandidates(epoch iotago.EpochIndex, expectedAccounts []iotago.AccountID, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			candidates, err := node.Protocol.Engines.Main.Get().SybilProtection.EligibleValidators(epoch)
			candidateIDs := lo.Map(candidates, func(candidate *accounts.AccountData) iotago.AccountID {
				return candidate.ID()
			})
			if err != nil {
				return ierrors.Wrapf(err, "AssertSybilProtectionCandidates: %s: failed to get eligible validators in epoch %d", node.Name, epoch)
			}

			if !assert.ElementsMatch(t.fakeTesting, expectedAccounts, candidateIDs) {
				return ierrors.Errorf("AssertSybilProtectionCandidates: %s: expected %s, got %s", node.Name, expectedAccounts, candidateIDs)
			}

			if len(expectedAccounts) != len(candidates) {
				return ierrors.Errorf("AssertSybilProtectionCandidates: %s: expected %v, got %v", node.Name, len(expectedAccounts), len(candidateIDs))
			}

			return nil
		})
	}
}

func (t *TestSuite) AssertSybilProtectionOnlineCommittee(expectedSeats []account.SeatIndex, nodes ...*mock.Node) {
	mustNodes(nodes)

	for _, node := range nodes {
		t.Eventually(func() error {
			seats := node.Protocol.Engines.Main.Get().SybilProtection.SeatManager().OnlineCommittee().ToSlice()
			if !assert.ElementsMatch(t.fakeTesting, expectedSeats, seats) {
				return ierrors.Errorf("AssertSybilProtectionOnlineCommittee: %s: expected %v, got %v", node.Name, expectedSeats, seats)
			}

			return nil
		})
	}
}
