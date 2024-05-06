//go:build dockertests

package dockertestframework

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (d *DockerTestFramework) AssertIndexerAccount(account *mock.AccountData) {
	d.Eventually(func() error {
		ctx := context.TODO()
		indexerClt, err := d.defaultWallet.Client.Indexer(ctx)
		if err != nil {
			return err
		}

		outputID, output, _, err := indexerClt.Account(ctx, account.Address)
		if err != nil {
			return err
		}

		assert.EqualValues(d.fakeTesting, account.OutputID, *outputID)
		assert.EqualValues(d.fakeTesting, account.Output, output)

		return nil
	})
}

func (d *DockerTestFramework) AssertIndexerFoundry(foundryID iotago.FoundryID) {
	d.Eventually(func() error {
		ctx := context.TODO()
		indexerClt, err := d.defaultWallet.Client.Indexer(ctx)
		if err != nil {
			return err
		}

		_, _, _, err = indexerClt.Foundry(ctx, foundryID)
		if err != nil {
			return err
		}

		return nil
	})
}

func (d *DockerTestFramework) AssertValidatorExists(accountAddr *iotago.AccountAddress) {
	d.Eventually(func() error {
		for _, node := range d.Nodes() {
			_, err := d.Client(node.Name).Validator(context.TODO(), accountAddr)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func (d *DockerTestFramework) AssertCommittee(expectedEpoch iotago.EpochIndex, expectedCommitteeMember []string) {
	fmt.Println("Wait for committee selection..., expected epoch: ", expectedEpoch, ", expected committee size: ", len(expectedCommitteeMember))
	defer fmt.Println("Wait for committee selection......done")

	sort.Strings(expectedCommitteeMember)

	status := d.NodeStatus("V1")
	testAPI := d.defaultWallet.Client.CommittedAPI()
	expectedSlotStart := testAPI.TimeProvider().EpochStart(expectedEpoch)
	require.Greater(d.Testing, expectedSlotStart, status.LatestAcceptedBlockSlot)

	if status.LatestAcceptedBlockSlot < expectedSlotStart {
		slotToWait := expectedSlotStart - status.LatestAcceptedBlockSlot
		secToWait := time.Duration(slotToWait) * time.Duration(testAPI.ProtocolParameters().SlotDurationInSeconds()) * time.Second
		fmt.Println("Wait for ", secToWait, "until expected epoch: ", expectedEpoch)
		time.Sleep(secToWait)
	}

	d.Eventually(func() error {
		for _, node := range d.Nodes() {
			resp, err := d.Client(node.Name).Committee(context.TODO())
			if err != nil {
				return err
			}

			if resp.Epoch == expectedEpoch {
				members := make([]string, len(resp.Committee))
				for i, member := range resp.Committee {
					members[i] = member.AddressBech32
				}

				sort.Strings(members)
				if match := lo.Equal(expectedCommitteeMember, members); match {
					return nil
				}

				return ierrors.Errorf("committee members does not match as expected, expected: %v, actual: %v", expectedCommitteeMember, members)
			}
		}

		return nil
	})
}

func (d *DockerTestFramework) AssertFinalizedSlot(condition func(iotago.SlotIndex) error) {
	for _, node := range d.Nodes() {
		status := d.NodeStatus(node.Name)

		err := condition(status.LatestFinalizedSlot)
		require.NoError(d.Testing, err)
	}
}
