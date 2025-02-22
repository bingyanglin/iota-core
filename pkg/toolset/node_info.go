package toolset

import (
	"context"
	"fmt"
	"os"

	flag "github.com/spf13/pflag"

	"github.com/iotaledger/hive.go/app/configuration"
	"github.com/iotaledger/iota.go/v4/nodeclient"
)

func nodeInfo(args []string) error {
	fs := configuration.NewUnsortedFlagSet("", flag.ContinueOnError)
	nodeURLFlag := fs.String(FlagToolNodeURL, "http://localhost:14265", "URL of the node (optional)")
	outputJSONFlag := fs.Bool(FlagToolOutputJSON, false, FlagToolDescriptionOutputJSON)

	fs.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage of %s:\n", ToolNodeInfo)
		fs.PrintDefaults()
		println(fmt.Sprintf("\nexample: %s --%s %s",
			ToolNodeInfo,
			FlagToolNodeURL,
			"http://192.168.1.221:14265",
		))
	}

	if err := parseFlagSet(fs, args); err != nil {
		return err
	}

	client, err := nodeclient.New(*nodeURLFlag)
	if err != nil {
		return err
	}

	info, err := client.Info(context.Background())
	if err != nil {
		return err
	}

	if *outputJSONFlag {
		return printJSON(info)
	}

	fmt.Printf("Name: %s\nVersion: %s\nLatestAcceptedBlockSlot: %d\nLatestConfirmedBlockSlot: %d\nIsHealthy: %s\n", info.Name, info.Version, info.Status.LatestAcceptedBlockSlot, info.Status.LatestConfirmedBlockSlot, yesOrNo(info.Status.IsHealthy))

	return nil
}
