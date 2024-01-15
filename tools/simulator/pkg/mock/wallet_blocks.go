package mock

import (
	"context"

	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
)

func (w *Wallet) IssueBasicBlock(ctx context.Context, opts ...options.Option[BasicBlockParams]) *blocks.Block {
	return w.BlockIssuer.IssueBasicBlock(ctx, w.Node, opts...)
}
