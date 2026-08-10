//go:build ignore

// Command generate_sandbox_runtime_canary_config emits one complete, immutable sandbox-runtime graph
// with only the selected sandbox integration and submission switches enabled.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"axiom/internal/config"
)

func main() {
	exchange := flag.String("exchange", "", "binance or bybit")
	output := flag.String("output", "", "absolute output path")
	flag.Parse()
	mode := config.ModeTestnet
	if *exchange == "bybit" {
		mode = config.ModeDemo
	} else if *exchange != "binance" {
		fail()
	}
	if *output == "" || (*output)[0] != '/' || flag.NArg() != 0 {
		fail()
	}
	configuration, err := config.DefaultSandboxConfiguration(mode)
	if err != nil {
		fail()
	}
	configuration.Sandbox.IntegrationsEnabled = true
	configuration.Sandbox.SubmissionEnabled = true
	for index := range configuration.Sandbox.Exchanges {
		selected := configuration.Sandbox.Exchanges[index].ID == *exchange
		configuration.Sandbox.Exchanges[index].IntegrationEnabled = selected
		configuration.Sandbox.Exchanges[index].SubmissionEnabled = selected
	}
	if config.Validate(configuration) != nil {
		fail()
	}
	encoded, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		fail()
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(
		*output,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o440,
	)
	if err != nil {
		fail()
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		fail()
	}
}

func fail() {
	fmt.Fprintln(os.Stderr, "sandbox_runtime_canary_configuration_generation_failed")
	os.Exit(1)
}
