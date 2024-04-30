package toolset

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mr-tron/base58"
	flag "github.com/spf13/pflag"

	"github.com/iotaledger/hive.go/app/configuration"
	hivecrypto "github.com/iotaledger/hive.go/crypto"
	"github.com/iotaledger/hive.go/crypto/pem"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota.go/v4/hexutil"
)

func generateP2PIdentity(args []string) error {
	fs := configuration.NewUnsortedFlagSet("", flag.ContinueOnError)
	privKeyFilePath := fs.String(FlagToolIdentityPrivateKeyFilePath, DefaultValueIdentityPrivateKeyFilePath, "the file path to the identity private key file")
	privateKeyFlag := fs.String(FlagToolPrivateKey, "", "the p2p private key")
	outputJSONFlag := fs.Bool(FlagToolOutputJSON, false, FlagToolDescriptionOutputJSON)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", ToolP2PIdentityGen)
		fs.PrintDefaults()
		println(fmt.Sprintf("\nexample: %s --%s %s --%s %s",
			ToolP2PIdentityGen,
			FlagToolIdentityPrivateKeyFilePath,
			DefaultValueIdentityPrivateKeyFilePath,
			FlagToolPrivateKey,
			"[PRIVATE_KEY]",
		))
	}

	if err := parseFlagSet(fs, args); err != nil {
		return err
	}

	if len(*privKeyFilePath) == 0 {
		return ierrors.Errorf("'%s' not specified", FlagToolIdentityPrivateKeyFilePath)
	}

	if err := os.MkdirAll(filepath.Dir(*privKeyFilePath), 0700); err != nil {
		return ierrors.Wrapf(err, "could not create peer store database dir '%s'", filepath.Dir(*privKeyFilePath))
	}

	_, err := os.Stat(*privKeyFilePath)
	switch {
	case err == nil || os.IsExist(err):
		// private key file already exists
		return ierrors.Errorf("private key file (%s) already exists", *privKeyFilePath)

	case os.IsNotExist(err):
		// private key file does not exist, create a new one

	default:
		return ierrors.Wrapf(err, "unable to check private key file (%s)", *privKeyFilePath)
	}

	var privKey ed25519.PrivateKey
	if privateKeyFlag != nil && len(*privateKeyFlag) > 0 {
		privKey, err = hivecrypto.ParseEd25519PrivateKeyFromString(*privateKeyFlag)
		if err != nil {
			return ierrors.Wrapf(err, "invalid private key given '%s'", *privateKeyFlag)
		}
	} else {
		// create identity
		_, privKey, err = ed25519.GenerateKey(nil)
		if err != nil {
			return ierrors.Wrap(err, "unable to generate Ed25519 private key for peer identity")
		}
	}

	libp2pPrivKey, libp2pPubKey, err := crypto.KeyPairFromStdKey(&privKey)
	if err != nil {
		return ierrors.Wrapf(err, "unable to convert given private key '%s'", hexutil.EncodeHex(privKey))
	}

	if err := pem.WriteEd25519PrivateKeyToPEMFile(*privKeyFilePath, privKey); err != nil {
		return ierrors.Wrap(err, "writing private key file for peer identity failed")
	}

	return printP2PIdentity(libp2pPrivKey, libp2pPubKey, *outputJSONFlag)
}

func printP2PIdentity(libp2pPrivKey crypto.PrivKey, libp2pPubKey crypto.PubKey, outputJSON bool) error {
	type P2PIdentity struct {
		PrivateKey      string `json:"privateKey"`
		PublicKey       string `json:"publicKey"`
		PublicKeyBase58 string `json:"publicKeyBase58"`
		PeerID          string `json:"peerId"`
	}

	privKeyBytes, err := libp2pPrivKey.Raw()
	if err != nil {
		return ierrors.Wrap(err, "unable to get raw private key bytes")
	}

	pubKeyBytes, err := libp2pPubKey.Raw()
	if err != nil {
		return ierrors.Wrap(err, "unable to get raw public key bytes")
	}

	peerID, err := peer.IDFromPublicKey(libp2pPubKey)
	if err != nil {
		return ierrors.Wrap(err, "unable to get peer identity from public key")
	}

	identity := P2PIdentity{
		PrivateKey:      hex.EncodeToString(privKeyBytes),
		PublicKey:       hex.EncodeToString(pubKeyBytes),
		PublicKeyBase58: base58.Encode(pubKeyBytes),
		PeerID:          peerID.String(),
	}

	if outputJSON {
		return printJSON(identity)
	}

	fmt.Println("Your p2p private key (hex):   ", identity.PrivateKey)
	fmt.Println("Your p2p public key (hex):    ", identity.PublicKey)
	fmt.Println("Your p2p public key (base58): ", identity.PublicKeyBase58)
	fmt.Println("Your p2p PeerID:              ", identity.PeerID)

	return nil
}
