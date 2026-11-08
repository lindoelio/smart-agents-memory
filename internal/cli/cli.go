// Package cli is the command line entrypoint of Smart Agents Memory.
//
// It parses the arguments, calls the use cases and prints the result. All
// the behaviour lives in the application layer, so another entrypoint — an
// MCP server, for instance — can serve the same use cases.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/app"
	"github.com/lindoelio/smart-agents-memory/internal/config"
	"github.com/lindoelio/smart-agents-memory/internal/version"
)

// legacyFlags maps the flags of the previous release to the current ones,
// so the muscle memory of an old script gets a pointer instead of a wall.
var legacyFlags = map[string]string{
	"--escopo":  "--scope",
	"--titulo":  "--title",
	"--corpo":   "--body",
	"--projeto": "--project",
	"--fonte":   "--source",
}

// showVersion tells the command line to print the version and stop.
var showVersion bool

// Run executes one invocation and returns the exit code.
func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	opts := config.Options{}
	global := flag.NewFlagSet(version.Name, flag.ContinueOnError)
	global.SetOutput(errOut)
	global.Usage = func() { printUsage(out) }
	registerGlobals(global, &opts)

	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(out)
			return 0
		}
		return 2
	}

	// The version is asked for before anything else: it must work
	// without a store, a configuration, or a network.
	if showVersion {
		fmt.Fprintln(out, version.Version)
		return 0
	}

	rest := global.Args()
	if len(rest) == 0 {
		printUsage(out)
		return 0
	}

	command, commandArgs := rest[0], rest[1:]
	if command == "help" {
		printUsage(out)
		return 0
	}
	if command == "version" {
		fmt.Fprintln(out, version.Version)
		return 0
	}

	if legacy := legacyHints(commandArgs); legacy != "" {
		fmt.Fprintln(errOut, legacy)
		return 2
	}

	switch command {
	case "search":
		return cmdSearch(ctx, opts, commandArgs, out, errOut)
	case "brief":
		return cmdBrief(ctx, opts, commandArgs, out, errOut)
	case "add":
		return cmdAdd(ctx, opts, commandArgs, out, errOut)
	case "log":
		return cmdLog(ctx, opts, commandArgs, out, errOut)
	case "index":
		return cmdIndex(ctx, opts, commandArgs, out, errOut)
	case "install":
		return cmdInstall(ctx, opts, commandArgs, out, errOut)
	case "doctor":
		return cmdDoctor(ctx, opts, commandArgs, out, errOut)
	case "path":
		return cmdPath(opts, commandArgs, out, errOut)
	case "migrate":
		return cmdMigrate(ctx, opts, commandArgs, out, errOut)
	default:
		fmt.Fprintf(errOut, "unknown command %q\n\n", command)
		printUsage(errOut)
		return 2
	}
}

// build resolves the configuration and opens the application.
func build(opts config.Options, out, errOut io.Writer) (*app.App, error) {
	cfg, configPath, err := config.Resolve(opts)
	if err != nil {
		return nil, err
	}
	return app.New(cfg, configPath, out, errOut)
}

// registerGlobals adds the global flags to a flag set.
//
// The current value is the default: the global flags are parsed once before
// the command, and again by the command itself, and the second parse must
// not wipe what the first one found.
func registerGlobals(fs *flag.FlagSet, opts *config.Options) {
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")
	fs.StringVar(&opts.Vault, "vault", opts.Vault, "storage gateway: obsidian (default), disk, sqlite, notion")
	fs.StringVar(&opts.MemoryHome, "memory-home", opts.MemoryHome, "root of the store")
	fs.StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "configuration file")
	fs.StringVar(&opts.Database, "database", opts.Database, "database of the sqlite gateway")
	fs.StringVar(&opts.NotionToken, "notion-token", opts.NotionToken, "token of the notion gateway")
	fs.StringVar(&opts.NotionDatabase, "notion-database", opts.NotionDatabase, "database id of the notion gateway")
}

// newFlagSet builds a flag set for a command, with the global flags too.
func newFlagSet(name string, out, errOut io.Writer, opts *config.Options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	registerGlobals(fs, opts)
	return fs
}

// legacyHints returns the pointer for a legacy flag, when there is one.
func legacyHints(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name := "--" + strings.TrimLeft(a, "-")
		if repl, ok := legacyFlags[name]; ok {
			return fmt.Sprintf("unknown flag %s; did you mean %s?", name, repl)
		}
	}
	return ""
}

// cmdSearch searches the corpus.
func cmdSearch(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("search", out, errOut, &opts)
	limit := fs.Int("limit", 30, "maximum number of results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	query := strings.TrimSpace(fs.Arg(0))
	if query == "" {
		fmt.Fprintln(errOut, "error: a query is required")
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	return searchAndPrint(ctx, a, query, *limit, out, errOut)
}

// searchAndPrint runs a search and prints its matches.
func searchAndPrint(ctx context.Context, a *app.App, query string, limit int, out, errOut io.Writer) int {
	hits, err := a.Search(ctx, query, limit)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	if len(hits) == 0 {
		fmt.Fprintf(out, "nothing found for %q\n", query)
		return 0
	}
	shown := hits
	omitted := 0
	if len(shown) > limit {
		omitted = len(shown) - limit
		shown = shown[:limit]
	}
	for _, hit := range shown {
		if hit.Line > 0 {
			fmt.Fprintf(out, "%s:%d: %s\n", hit.Path, hit.Line, strings.TrimSpace(hit.Text))
			continue
		}
		fmt.Fprintf(out, "%s: %s\n", hit.Path, strings.TrimSpace(hit.Text))
	}
	if omitted > 0 {
		fmt.Fprintf(out, "\n(%d more matches omitted)\n", omitted)
	}
	return 0
}

// cmdBrief prints the context package.
func cmdBrief(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("brief", out, errOut, &opts)
	project := fs.String("project", "", "notes of a project")
	query := fs.String("query", "", "also search for this")
	limit := fs.Int("limit", 20, "maximum number of results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	text, err := a.Brief(ctx, *project, *query, *limit)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	if text != "" {
		fmt.Fprintln(out, text)
	}
	if *query != "" {
		return searchAndPrint(ctx, a, *query, *limit, out, errOut)
	}
	return 0
}

// cmdAdd records one durable memory.
func cmdAdd(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("add", out, errOut, &opts)
	scope := fs.String("scope", "lesson", "user, lesson, project or reference")
	title := fs.String("title", "", "title of the entry")
	body := fs.String("body", "", "content of the entry")
	source := fs.String("source", "", "where the entry came from")
	native := fs.String("native", "", "minimax-user, minimax-agent or qwen")
	tags := fs.String("tags", "", "comma separated keywords")
	force := fs.Bool("force", false, "write even when the title already exists")
	allow := fs.Bool("allow-other-language", false, "skip the English-default warning")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*title) == "" {
		fmt.Fprintln(errOut, "error: --title is required")
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	message, err := a.Add(ctx, app.AddOptions{
		Scope:              *scope,
		Title:              *title,
		Body:               *body,
		Source:             *source,
		Native:             *native,
		Tags:               splitTags(*tags),
		Force:              *force,
		AllowOtherLanguage: *allow,
	})
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	fmt.Fprintln(out, message)
	return 0
}

// cmdLog prints the write log.
func cmdLog(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("log", out, errOut, &opts)
	limit := fs.Int("limit", 20, "maximum number of lines")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	lines, err := a.Log(ctx, *limit)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
	return 0
}

// cmdIndex reconciles the corpus with the native projections.
func cmdIndex(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("index", out, errOut, &opts)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	report, err := a.Index(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	index := report.IndexPath
	if index == "" {
		index = "index"
	}
	fmt.Fprintf(out, "index: %s (%d entries)\n", index, report.Entries)
	if report.Imported > 0 {
		fmt.Fprintf(out, "  imported from native: %d\n", report.Imported)
	}
	if report.Updated > 0 {
		fmt.Fprintf(out, "  updated from native: %d\n", report.Updated)
	}
	if report.Exported > 0 {
		fmt.Fprintf(out, "  projected to native: %d\n", report.Exported)
	}
	if report.Imported == 0 && report.Updated == 0 && report.Exported == 0 {
		fmt.Fprintln(out, "  nothing to reconcile")
	}
	return 0
}

// cmdInstall links the tools to the store.
func cmdInstall(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("install", out, errOut, &opts)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	result, err := a.Install(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	for _, line := range result.Lines {
		fmt.Fprintln(out, "[ok] "+line)
	}
	return 0
}

// cmdDoctor checks the installation.
func cmdDoctor(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("doctor", out, errOut, &opts)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	report, err := a.Doctor(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	for _, note := range report.Notes {
		fmt.Fprintln(out, note)
	}
	for _, problem := range report.Problems {
		fmt.Fprintln(errOut, problem)
	}
	if !report.OK() {
		fmt.Fprintf(errOut, "\n%d problem(s). run `smart-agents-memory index` and `smart-agents-memory install` to repair.\n", len(report.Problems))
		return 1
	}
	fmt.Fprintln(out, "\nno problems found")
	return 0
}

// cmdPath prints the root of the store.
func cmdPath(opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("path", out, errOut, &opts)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	fmt.Fprintln(out, a.Path())
	return 0
}

// cmdMigrate brings a legacy store to the current layout.
func cmdMigrate(ctx context.Context, opts config.Options, args []string, out, errOut io.Writer) int {
	fs := newFlagSet("migrate", out, errOut, &opts)
	apply := fs.Bool("apply", false, "make the changes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	a, err := build(opts, out, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	changes, err := a.Migrate(ctx, *apply)
	if err != nil {
		fmt.Fprintln(errOut, "error: "+err.Error())
		return 1
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, "nothing to migrate")
		return 0
	}
	for _, change := range changes {
		fmt.Fprintf(out, "%s: %s\n", change.Path, change.Action)
	}
	if !*apply {
		fmt.Fprintf(out, "\n%d change(s). run again with --apply.\n", len(changes))
	}
	return 0
}

// splitTags splits a comma separated list of keywords.
func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// printUsage prints the help of the product.
func printUsage(out io.Writer) {
	fmt.Fprintf(out, `%s — shared memory for AI agents.

One corpus, many harnesses: the memory lives in a single store, every tool
reads it in the format it expects, and the CLI is the only way to write it.

Usage:
  %s [global flags] <command> [flags]

Commands:
  search <query>       search the corpus
  brief                the profile, the rules, and optionally a project
  add                  record one durable memory
  log                  the write log, newest first
  index                reconcile the corpus with the native projections
  install              link the tools to the store
  doctor               check the links, the projections and the store
  path                 print the root of the store
  migrate              bring a legacy store to the current layout
  version              print the version

Global flags:
  --version                print the version and exit
  --vault <name>           storage gateway: obsidian (default), disk, sqlite, notion
  --memory-home <path>     root of the store
  --config <path>          configuration file
  --database <path>        database of the sqlite gateway
  --notion-token <token>   token of the notion gateway
  --notion-database <id>   database id of the notion gateway

Environment:
  %s          root of the store
  %s      backend
  %s   source recorded on new entries
  %s   token of the notion gateway

Examples:
  %s search "neon"
  %s brief --project my-project
  %s add --scope lesson --title "Short title" --body "Rule... Why..."
  %s install
`, version.Name, version.Name,
		config.EnvHome, config.EnvVault, config.EnvSource, config.EnvNotionToken,
		version.Name, version.Name, version.Name, version.Name)
}
