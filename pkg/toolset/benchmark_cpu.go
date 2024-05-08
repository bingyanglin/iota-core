package toolset

import (
	"context"
	"crypto"
	"sync/atomic"

	"golang.org/x/crypto/blake2b"
)

const (
	// Hash defines the hash function that is used to compute the PoW digest.
	Hash = crypto.BLAKE2b_256
)

func cpuBenchmarkWorker(ctx context.Context, powDigest []byte, counter *uint64) {
	for {
		select {
		case <-ctx.Done():
			return

		default:
			result := blake2b.Sum256(powDigest)
			powDigest = result[:]
			atomic.AddUint64(counter, 1)
		}
	}
}
