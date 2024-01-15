package core

import (
	"os"
	"time"

	"github.com/iotaledger/hive.go/log"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/mock"
	"github.com/iotaledger/iota-core/tools/simulator/pkg/snapshotcreator"
	iotago "github.com/iotaledger/iota.go/v4"
)

func WithWaitFor(waitFor time.Duration) options.Option[Simulator] {
	return func(t *Simulator) {
		t.optsWaitFor = waitFor
	}
}

func WithTick(tick time.Duration) options.Option[Simulator] {
	return func(t *Simulator) {
		t.optsTick = tick
	}
}

func WithAccounts(accounts ...snapshotcreator.AccountDetails) options.Option[Simulator] {
	return func(t *Simulator) {
		t.optsAccounts = append(t.optsAccounts, accounts...)
	}
}

func WithNetworkOptions(networkOptions ...options.Option[mock.Network]) options.Option[Simulator] {
	return func(t *Simulator) {
		t.optsNetworkOptions = networkOptions
	}
}

func WithSnapshotOptions(snapshotOptions ...options.Option[snapshotcreator.Options]) options.Option[Simulator] {
	return func(t *Simulator) {
		t.optsSnapshotOptions = snapshotOptions
	}
}

func WithProtocolParametersOptions(protocolParameterOptions ...options.Option[iotago.V3ProtocolParameters]) options.Option[Simulator] {
	return func(t *Simulator) {
		t.ProtocolParameterOptions = protocolParameterOptions
	}
}

func WithLogger(logger log.Logger) options.Option[Simulator] {
	return func(t *Simulator) {
		t.optsLogger = logger
	}
}

func GenesisTimeWithOffsetBySlots(slots iotago.SlotIndex, slotDurationInSeconds uint8) int64 {
	return time.Now().Truncate(time.Duration(slotDurationInSeconds)*time.Second).Unix() - int64(slotDurationInSeconds)*int64(slots)
}

func durationFromEnvOrDefault(defaultDuration time.Duration, envKey string) time.Duration {
	waitFor := os.Getenv(envKey)
	if waitFor == "" {
		return defaultDuration
	}

	d, err := time.ParseDuration(waitFor)
	if err != nil {
		panic(err)
	}

	return d
}

var (
	defaultProtocolParams              = iotago.NewV3SnapshotProtocolParameters()
	DefaultSlotDurationInSeconds uint8 = defaultProtocolParams.SlotDurationInSeconds()
	DefaultSlotsPerEpochExponent uint8 = defaultProtocolParams.SlotsPerEpochExponent()

	DefaultLivenessThresholdLowerBoundInSeconds uint16           = uint16(defaultProtocolParams.LivenessThresholdLowerBound().Seconds())
	DefaultLivenessThresholdUpperBoundInSeconds uint16           = uint16(defaultProtocolParams.LivenessThresholdUpperBound().Seconds())
	DefaultMinCommittableAge                    iotago.SlotIndex = defaultProtocolParams.MinCommittableAge()
	DefaultMaxCommittableAge                    iotago.SlotIndex = defaultProtocolParams.MaxCommittableAge()
	DefaultEpochNearingThreshold                iotago.SlotIndex = defaultProtocolParams.EpochNearingThreshold()

	DefaultMinReferenceManaCost iotago.Mana      = defaultProtocolParams.CongestionControlParameters().MinReferenceManaCost
	DefaultRMCIncrease          iotago.Mana      = defaultProtocolParams.CongestionControlParameters().Increase
	DefaultRMCDecrease          iotago.Mana      = defaultProtocolParams.CongestionControlParameters().Decrease
	DefaultRMCIncreaseThreshold iotago.WorkScore = defaultProtocolParams.CongestionControlParameters().IncreaseThreshold
	DefaultRMCDecreaseThreshold iotago.WorkScore = defaultProtocolParams.CongestionControlParameters().DecreaseThreshold
	DefaultSchedulerRate        iotago.WorkScore = defaultProtocolParams.CongestionControlParameters().SchedulerRate
	DefaultMaxBufferSize        uint32           = defaultProtocolParams.CongestionControlParameters().MaxBufferSize
	DefaultMaxValBufferSize     uint32           = defaultProtocolParams.CongestionControlParameters().MaxValidationBufferSize
)
