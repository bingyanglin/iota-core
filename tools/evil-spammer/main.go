package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iotaledger/iota-core/tools/evil-spammer/logger"
	"github.com/iotaledger/iota-core/tools/evil-spammer/wallet"
)

var (
	log           = logger.New("main")
	optionFlagSet = flag.NewFlagSet("script flag set", flag.ExitOnError)
)

func main() {
	help := parseFlags()

	if help {
		fmt.Println("Usage of the Evil Spammer tool, provide the first argument for the selected mode:\n" +
			"'interactive' - enters the interactive mode.\n" +
			"'basic' - can be parametrized with additional flags to run one time spammer. Run 'evil-wallet basic -h' for the list of possible flags.\n" +
			"'quick' - runs simple stress test: tx spam -> blk spam -> ds spam. Run 'evil-wallet quick -h' for the list of possible flags.\n" +
			"'commitments' - runs spammer for commitments. Run 'evil-wallet commitments -h' for the list of possible flags.")

		return
	}
	// run selected test scenario
	switch Script {
	case "interactive":
		Run()
	case "basic":
		CustomSpam(&customSpamParams)
	case "quick":
		QuickTest(&quickTestParams)
	// case "commitments":
	// 	CommitmentsSpam(&commitmentsSpamParams)
	default:
		log.Warnf("Unknown parameter for script, possible values: basic, quick, commitments")
	}
}

func parseFlags() (help bool) {
	if len(os.Args) <= 1 {
		return true
	}
	script := os.Args[1]

	Script = script
	log.Infof("script %s", Script)

	switch Script {
	case "basic":
		parseBasicSpamFlags()
	case "quick":
		parseQuickTestFlags()
		// case "commitments":
		// 	parseCommitmentsSpamFlags()
	}
	if Script == "help" || Script == "-h" || Script == "--help" {
		return true
	}

	return
}

func parseOptionFlagSet(flagSet *flag.FlagSet) {
	err := flagSet.Parse(os.Args[2:])
	if err != nil {
		log.Errorf("Cannot parse first `script` parameter")
		return
	}
}

func parseBasicSpamFlags() {
	urls := optionFlagSet.String("urls", "", "API urls for clients used in test separated with commas")
	spamTypes := optionFlagSet.String("spammer", "", "Spammers used during test. Format: strings separated with comma, available options: 'blk' - block,"+
		" 'tx' - transaction, 'ds' - double spends spammers, 'nds' - n-spends spammer, 'custom' - spams with provided scenario")
	rate := optionFlagSet.String("rate", "", "Spamming rate for provided 'spammer'. Format: numbers separated with comma, e.g. 10,100,1 if three spammers were provided for 'spammer' parameter.")
	duration := optionFlagSet.String("duration", "", "Spam duration. Cannot be combined with flag 'blkNum'. Format: separated by commas list of decimal numbers, each with optional fraction and a unit suffix, such as '300ms', '-1.5h' or '2h45m'.\n Valid time units are 'ns', 'us', 'ms', 's', 'm', 'h'.")
	blkNum := optionFlagSet.String("blkNum", "", "Spam duration in seconds. Cannot be combined with flag 'duration'. Format: numbers separated with comma, e.g. 10,100,1 if three spammers were provided for 'spammer' parameter.")
	timeunit := optionFlagSet.Duration("tu", customSpamParams.TimeUnit, "Time unit for the spamming rate. Format: decimal numbers, each with optional fraction and a unit suffix, such as '300ms', '-1.5h' or '2h45m'.\n Valid time units are 'ns', 'us', 'ms', 's', 'm', 'h'.")
	delayBetweenConflicts := optionFlagSet.Duration("dbc", customSpamParams.DelayBetweenConflicts, "delayBetweenConflicts - Time delay between conflicts in double spend spamming")
	scenario := optionFlagSet.String("scenario", "", "Name of the EvilBatch that should be used for the spam. By default uses Scenario1. Possible scenarios can be found in evilwallet/customscenarion.go.")
	deepSpam := optionFlagSet.Bool("deep", customSpamParams.DeepSpam, "Enable the deep spam, by reusing outputs created during the spam.")
	nSpend := optionFlagSet.Int("nSpend", customSpamParams.NSpend, "Number of outputs to be spent in n-spends spammer for the spammer type needs to be set to 'ds'. Default value is 2 for double-spend.")

	parseOptionFlagSet(optionFlagSet)

	if *urls != "" {
		parsedUrls := parseCommaSepString(*urls)
		quickTestParams.ClientURLs = parsedUrls
		customSpamParams.ClientURLs = parsedUrls
	}
	if *spamTypes != "" {
		parsedSpamTypes := parseCommaSepString(*spamTypes)
		customSpamParams.SpamTypes = parsedSpamTypes
	}
	if *rate != "" {
		parsedRates := parseCommaSepInt(*rate)
		customSpamParams.Rates = parsedRates
	}
	if *duration != "" {
		parsedDurations := parseDurations(*duration)
		customSpamParams.Durations = parsedDurations
	}
	if *blkNum != "" {
		parsedBlkNums := parseCommaSepInt(*blkNum)
		customSpamParams.BlkToBeSent = parsedBlkNums
	}
	if *scenario != "" {
		conflictBatch, ok := wallet.GetScenario(*scenario)
		if ok {
			customSpamParams.Scenario = conflictBatch
		}
	}

	customSpamParams.NSpend = *nSpend
	customSpamParams.DeepSpam = *deepSpam
	customSpamParams.TimeUnit = *timeunit
	customSpamParams.DelayBetweenConflicts = *delayBetweenConflicts

	// fill in unused parameter: blkNum or duration with zeros
	if *duration == "" && *blkNum != "" {
		customSpamParams.Durations = make([]time.Duration, len(customSpamParams.BlkToBeSent))
	}
	if *blkNum == "" && *duration != "" {
		customSpamParams.BlkToBeSent = make([]int, len(customSpamParams.Durations))
	}

	customSpamParams.config = loadBasicConfig()
}

func parseQuickTestFlags() {
	urls := optionFlagSet.String("urls", "", "API urls for clients used in test separated with commas")
	rate := optionFlagSet.Int("rate", quickTestParams.Rate, "The spamming rate")
	duration := optionFlagSet.Duration("duration", quickTestParams.Duration, "Duration of the spam. Format: decimal numbers, each with optional fraction and a unit suffix, such as '300ms', '-1.5h' or '2h45m'.\n Valid time units are 'ns', 'us', 'ms', 's', 'm', 'h'.")
	timeunit := optionFlagSet.Duration("tu", quickTestParams.TimeUnit, "Time unit for the spamming rate. Format: decimal numbers, each with optional fraction and a unit suffix, such as '300ms', '-1.5h' or '2h45m'.\n Valid time units are 'ns', 'us', 'ms', 's', 'm', 'h'.")
	delayBetweenConflicts := optionFlagSet.Duration("dbc", quickTestParams.DelayBetweenConflicts, "delayBetweenConflicts - Time delay between conflicts in double spend spamming")
	verifyLedger := optionFlagSet.Bool("verify", quickTestParams.VerifyLedger, "Set to true if verify ledger script should be run at the end of the test")

	parseOptionFlagSet(optionFlagSet)

	if *urls != "" {
		parsedUrls := parseCommaSepString(*urls)
		quickTestParams.ClientURLs = parsedUrls
	}
	quickTestParams.Rate = *rate
	quickTestParams.Duration = *duration
	quickTestParams.TimeUnit = *timeunit
	quickTestParams.DelayBetweenConflicts = *delayBetweenConflicts
	quickTestParams.VerifyLedger = *verifyLedger
}

// func parseCommitmentsSpamFlags() {
// 	commitmentType := optionFlagSet.String("type", commitmentsSpamParams.CommitmentType, "Type of commitment spam. Possible values: 'latest' - valid commitment spam, 'random' - completely new, invalid cahin, 'fork' - forked chain, combine with 'forkAfter' parameter.")
// 	rate := optionFlagSet.Int("rate", commitmentsSpamParams.Rate, "Commitment spam rate")
// 	duration := optionFlagSet.Duration("duration", commitmentsSpamParams.Duration, "Duration of the spam. Format: decimal numbers, each with optional fraction and a unit suffix, such as '300ms', '-1.5h' or '2h45m'.\n Valid time units are 'ns', 'us', 'ms', 's', 'm', 'h'.")
// 	timeUnit := optionFlagSet.Duration("tu", commitmentsSpamParams.TimeUnit, "Time unit for the spamming rate. Format: decimal numbers, each with optional fraction and a unit suffix, such as '300ms', '-1.5h' or '2h45m'.\n Valid time units are 'ns', 'us', 'ms', 's', 'm', 'h'.")
// 	networkAlias := optionFlagSet.String("network", commitmentsSpamParams.NetworkAlias, "Network alias for the test. Check your keys-config.json file for possible values.")
// 	identityAlias := optionFlagSet.String("spammerAlias", commitmentsSpamParams.SpammerAlias, "Identity alias for the node identity and its private keys that will be used to spam. Check your keys-config.json file for possible values.")
// 	validAlias := optionFlagSet.String("validAlias", commitmentsSpamParams.ValidAlias, "Identity alias for the honest node and its private keys, will be used to request valid commitment and block data. Check your keys-config.json file for possible values.")
// 	forkAfter := optionFlagSet.Int("forkAfter", commitmentsSpamParams.Rate, "Indicates how many slots after spammer startup should fork be placed in the created commitment chain. Works only for 'fork' commitment spam type.")

// 	parseOptionFlagSet(optionFlagSet)

// 	commitmentsSpamParams.CommitmentType = *commitmentType
// 	commitmentsSpamParams.Rate = *rate
// 	commitmentsSpamParams.Duration = *duration
// 	commitmentsSpamParams.TimeUnit = *timeUnit
// 	commitmentsSpamParams.NetworkAlias = *networkAlias
// 	commitmentsSpamParams.SpammerAlias = *identityAlias
// 	commitmentsSpamParams.ValidAlias = *validAlias
// 	commitmentsSpamParams.ForkAfter = *forkAfter
// }

func parseCommaSepString(urls string) []string {
	split := strings.Split(urls, ",")

	return split
}

func parseCommaSepInt(nums string) []int {
	split := strings.Split(nums, ",")
	parsed := make([]int, len(split))
	for i, num := range split {
		parsed[i], _ = strconv.Atoi(num)
	}

	return parsed
}

func parseDurations(durations string) []time.Duration {
	split := strings.Split(durations, ",")
	parsed := make([]time.Duration, len(split))
	for i, dur := range split {
		parsed[i], _ = time.ParseDuration(dur)
	}

	return parsed
}

type BasicConfig struct {
	LastFaucetUnspentOutputID string `json:"lastFaucetUnspentOutputId"`
}

var basicConfigJSON = `{
	"lastFaucetUnspentOutputId": ""
}`

var basicConfigFile = "basic_config.json"

// load the config file.
func loadBasicConfig() *BasicConfig {
	// open config file
	config := new(BasicConfig)
	file, err := os.Open(basicConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			panic(err)
		}

		//nolint:gosec // users should be able to read the file
		if err = os.WriteFile(basicConfigFile, []byte(basicConfigJSON), 0o644); err != nil {
			panic(err)
		}
		if file, err = os.Open(basicConfigFile); err != nil {
			panic(err)
		}
	}
	defer file.Close()

	// decode config file
	if err = json.NewDecoder(file).Decode(config); err != nil {
		panic(err)
	}

	return config
}

func saveConfigsToFile(config *BasicConfig) {
	// open config file
	file, err := os.Open(basicConfigFile)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	jsonConfigs, err := json.MarshalIndent(config, "", "    ")

	if err != nil {
		log.Errorf("failed to write configs to file %s", err)
	}

	//nolint:gosec // users should be able to read the file
	if err = os.WriteFile(basicConfigFile, jsonConfigs, 0o644); err != nil {
		panic(err)
	}
}
