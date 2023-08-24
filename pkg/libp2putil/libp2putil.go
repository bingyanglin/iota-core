package libp2putil

import (
	golibp2p "github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	p2ppeer "github.com/libp2p/go-libp2p/core/peer"

	"github.com/iotaledger/hive.go/autopeering/peer"
	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/pkg/network"
)

// GetLibp2pIdentity returns libp2p Host option for Identity from local peer object.
func GetLibp2pIdentity(lPeer *peer.Local) (golibp2p.Option, error) {
	ourPrivateKey, err := lPeer.Database().LocalPrivateKey()
	if err != nil {
		return nil, ierrors.WithStack(err)
	}

	libp2pPrivateKey, err := ToLibp2pPrivateKey(ourPrivateKey)
	if err != nil {
		return nil, ierrors.WithStack(err)
	}

	return golibp2p.Identity(libp2pPrivateKey), nil
}

// ToLibp2pPrivateKey transforms private key in our type to libp2p type.
func ToLibp2pPrivateKey(ourPrivateKey ed25519.PrivateKey) (libp2pcrypto.PrivKey, error) {
	privateKeyBytes, err := ourPrivateKey.Bytes()
	if err != nil {
		return nil, ierrors.WithStack(err)
	}

	libp2pPrivateKey, err := libp2pcrypto.UnmarshalEd25519PrivateKey(privateKeyBytes)
	if err != nil {
		return nil, ierrors.WithStack(err)
	}

	return libp2pPrivateKey, nil
}

// ToLibp2pPeerID computes libp2p peer ID from our peer object.
func ToLibp2pPeerID(p *network.Peer) (p2ppeer.ID, error) {
	pubKeyBytes, err := p.Identity.PublicKey().Bytes()
	if err != nil {
		return "", ierrors.WithStack(err)
	}

	pubKeyLibp2p, err := libp2pcrypto.UnmarshalEd25519PublicKey(pubKeyBytes)
	if err != nil {
		return "", ierrors.WithStack(err)
	}

	libp2pID, err := p2ppeer.IDFromPublicKey(pubKeyLibp2p)
	if err != nil {
		return "", ierrors.WithStack(err)
	}

	return libp2pID, nil
}
