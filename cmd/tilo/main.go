package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"tilo/internal/color"
	"tilo/internal/config"
	"tilo/internal/ui"
)

func main() {
	var configPath string
	var plain bool
	var follow bool
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.BoolVar(&plain, "plain", false, "disable color output")
	flag.BoolVar(&follow, "f", false, "follow file growth")
	flag.Parse()

	lines, followCh, err := readInput(flag.Args(), follow)
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
		if followCh != nil {
			for batch := range followCh {
				printNonInteractive(batch, colorRules, plain)
			}
		}
		return
	}

	statusAtTop := cfg.StatusBar == "top"
	lineNumbers := true
	if cfg.LineNumbers != nil {
		lineNumbers = *cfg.LineNumbers
	}
	if err := ui.Run(lines, colorRules, plain, statusAtTop, lineNumbers, follow, followCh); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readInput(args []string, follow bool) ([]string, <-chan []string, error) {
	if len(args) > 1 {
		return nil, nil, errors.New("usage: tilo [path|-]")
	}

	if len(args) == 0 {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			lines, err := readLines(os.Stdin)
			return lines, nil, err
		}
		return nil, nil, config.ErrNoInput
	}

	if args[0] == "-" {
		if follow {
			return nil, nil, errors.New("follow requires a file path")
		}
		lines, err := readLines(os.Stdin)
		return lines, nil, err
	}

	file, err := os.Open(args[0])
	if err != nil {
		return nil, nil, err
	}
	if !follow {
		defer file.Close()
		lines, err := readLines(file)
		return lines, nil, err
	}
	lines, err := readLines(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	ch := tailFile(file)
	return lines, ch, nil
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

func tailFile(file *os.File) <-chan []string {
	out := make(chan []string, 16)
	reader := bufio.NewReader(file)
	go func() {
		defer close(out)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				return
			}
			if line == "" {
				continue
			}
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			out <- []string{line}
		}
	}()
	return out
}
