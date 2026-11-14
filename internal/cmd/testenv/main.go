//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alexfalkowski/go-linux/internal/testenv"
)

func main() {
	options, err := testenv.FromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report, checkErr := testenv.Check(options)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if checkErr != nil {
		fmt.Fprintln(os.Stderr, checkErr)
		os.Exit(1)
	}
}
