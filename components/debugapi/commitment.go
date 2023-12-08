package debugapi

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"

	"github.com/iotaledger/hive.go/ds/walker"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/pkg/protocol"
)

func chainManagerAllChainsDot() (string, error) {
	rootCommitment := deps.Protocol.Chains.Main.Get().ForkingPoint.Get()
	g := graphviz.New()
	defer g.Close()

	graph, err := prepareCommitmentGraph(g, rootCommitment)
	if err != nil {
		return "", err
	}
	defer graph.Close()

	var buf bytes.Buffer
	if err := g.Render(graph, "dot", &buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func chainManagerAllChainsRendered() ([]byte, error) {
	rootCommitment := deps.Protocol.Chains.Main.Get().ForkingPoint.Get()
	g := graphviz.New()
	defer g.Close()

	graph, err := prepareCommitmentGraph(g, rootCommitment)
	if err != nil {
		return nil, err
	}
	defer graph.Close()

	var buf bytes.Buffer
	if err := g.Render(graph, graphviz.PNG, &buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func prepareCommitmentGraph(g *graphviz.Graphviz, rootCommitment *protocol.Commitment) (*cgraph.Graph, error) {
	graph, err := g.Graph()
	if err != nil {
		return nil, err
	}

	root, rootErr := createNode(graph, rootCommitment)
	if rootErr != nil {
		return nil, rootErr
	}
	root.SetColor("green")

	for commitmentWalker := walker.New[*protocol.Commitment](false).Push(rootCommitment); commitmentWalker.HasNext(); {
		parentCommitment := commitmentWalker.Next()
		parent, parentErr := createNode(graph, parentCommitment)
		if parentErr != nil {
			return nil, parentErr
		}

		if err = parentCommitment.Children.ForEach(func(childCommitment *protocol.Commitment) error {
			child, childErr := createNode(graph, childCommitment)
			if childErr != nil {
				return childErr
			}

			if childCommitment.Chain.Get() == deps.Protocol.Chains.Main.Get() {
				child.SetColor("green")
			}

			if _, edgeErr := graph.CreateEdge(fmt.Sprintf("%s -> %s", parentCommitment.ID().String()[:8], childCommitment.ID().String()[:8]), parent, child); edgeErr != nil {
				return ierrors.Wrapf(edgeErr, "could not create edge %s -> %s", parentCommitment.ID().String()[:8], childCommitment.ID().String()[:8])
			}

			commitmentWalker.Push(childCommitment)

			return nil
		}); err != nil {
			return nil, err
		}
	}

	return graph, nil
}

func createNode(graph *cgraph.Graph, commitment *protocol.Commitment) (*cgraph.Node, error) {
	node, err := graph.Node(fmt.Sprintf("%d: %s", commitment.ID().Slot(), commitment.ID().String()[:8]))
	if err != nil {
		return nil, ierrors.Wrapf(err, "could not create node %s", commitment.ID().String()[:8])
	}

	return node, nil
}
