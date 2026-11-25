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
	file := flag.String("file", "", "limgo JSON report")
	minLines := flag.Float64("min", 90, "minimum line coverage")
	flag.Parse()
	if *file == "" {
		fmt.Fprintln(os.Stderr, "coveragecheck: -file is required")
		os.Exit(2)
	}
	value, err := check(*file, *minLines)
	if err != nil {
		fmt.Fprintln(os.Stderr, "coveragecheck: "+err.Error())
		os.Exit(2)
	}
	fmt.Printf("lines: %.2f%% (minimum %.2f%%)\n", value, *minLines)
	if value < *minLines {
		os.Exit(1)
	}
}

func check(path string, min float64) (float64, error) {
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
