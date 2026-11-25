// Command coveragecheck enforces the line gate from a limgo JSON report.
//
// limgo v1.0.0 compares the branch coverage against the lines threshold,
// which makes a lines:90 configuration fail even when branches pass. The
// script therefore asks limgo for the report with a permissive lines
// threshold and enforces the real line minimum here.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
)

type report struct {
	Statistic struct {
		Coverage struct {
			Statements string `json:"stmts"`
			Lines      string `json:"lines"`
			Branches   string `json:"branches"`
		} `json:"coverage"`
	} `json:"statistic"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("coveragecheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "limgo JSON report")
	minLines := flags.Float64("min", 90, "minimum line coverage")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *file == "" {
		fmt.Fprintln(stderr, "coveragecheck: -file is required")
		return 2
	}
	value, err := check(*file)
	if err != nil {
		fmt.Fprintln(stderr, "coveragecheck: "+err.Error())
		return 2
	}
	fmt.Fprintf(stdout, "lines: %.2f%% (minimum %.2f%%)\n", value, *minLines)
	if value < *minLines {
		return 1
	}
	return 0
}

func check(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var out report
	if err := json.Unmarshal(data, &out); err != nil {
		return 0, err
	}
	raw := out.Statistic.Coverage.Lines
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse lines %q: %w", raw, err)
	}
	return value, nil
}
