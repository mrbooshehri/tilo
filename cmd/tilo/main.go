package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"tilo/internal/color"
	"tilo/internal/config"
	"tilo/internal/ui"
)

func main() {
	var configPath string
	var plain bool
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.BoolVar(&plain, "plain", false, "disable color output")
	flag.Parse()

	lines, err := readInput(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "no input")
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	defaults := color.BuildDefaultRules()
	custom := make([]color.CustomRule, 0, len(cfg.CustomRules))
	for _, rule := range cfg.CustomRules {
		custom = append(custom, color.CustomRule{
			Pattern: rule.Pattern,
			Color:   rule.Color,
			Style:   rule.Style,
		})
	}
	colorRules, err := color.BuildRules(defaults, cfg.Colors, cfg.DisableBuiltin, custom)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
		printNonInteractive(lines, colorRules, plain)
		return
	}

	if err := ui.Run(lines, colorRules, plain); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readInput(args []string) ([]string, error) {
	if len(args) > 1 {
		return nil, errors.New("usage: tilo [path|-]")
	}

	if len(args) == 0 {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return readLines(os.Stdin)
		}
		return nil, config.ErrNoInput
	}

	if args[0] == "-" {
		return readLines(os.Stdin)
	}

	file, err := os.Open(args[0])
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLines(file)
}

func readLines(r io.Reader) ([]string, error) {
	reader := bufio.NewReader(r)
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			lines = append(lines, line)
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return lines, nil
}

func printNonInteractive(lines []string, rules []color.Rule, plain bool) {
	for _, line := range lines {
		if !plain {
			line = color.ApplyRules(line, rules)
		}
		fmt.Fprintln(os.Stdout, line)
	}
}
