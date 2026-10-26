// Command memoryd is the MemoryTree command-line tool.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/li-ziyin/memorytree/internal/bench"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/initrepo"
	"github.com/li-ziyin/memorytree/internal/legacy"
	"github.com/li-ziyin/memorytree/internal/mcpserver"
	"github.com/li-ziyin/memorytree/internal/store"
	"github.com/li-ziyin/memorytree/internal/validate"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "import-legacy":
		return runImportLegacy(args[1:], stdout, stderr)
	case "index":
		return runIndex(args[1:], stdout, stderr)
	case "mcp":
		return runMCP(args[1:], stdin, stdout, stderr)
	case "bench":
		return runBench(args[1:], stdout, stderr)
	case "add":
		return runAdd(args[1:], stdin, stdout, stderr)
	case "supersede":
		return runSupersede(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "memoryd %s (commit %s, built %s)\n", version, commit, date)
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "memoryd: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "title of the root node")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd init [--title TITLE] <data-dir>")
		return 2
	}
	root, err := initrepo.Init(initrepo.Options{Root: fs.Arg(0), Title: *title})
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: init failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "initialized MemoryTree data repository at %s\n", root)
	return 0
}

func runImportLegacy(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import-legacy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "directory of legacy flat memory files")
	to := fs.String("to", "", "target MemoryTree data directory")
	agent := fs.String("agent", "zcode", "source agent recorded in provenance")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *from == "" || *to == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: memoryd import-legacy --from DIR --to DATA-DIR [--agent NAME]")
		return 2
	}
	stats, err := legacy.Import(legacy.Options{From: *from, To: *to, Agent: *agent})
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: import failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "imported %d entries into %s (%d skipped)\n", stats.Imported, stats.Project, stats.Skipped)
	return 0
}

func runIndex(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rebuild := fs.Bool("rebuild", false, "rebuild the index even when the source is unchanged")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd index [--rebuild] [--json] <data-dir>")
		return 2
	}

	res, err := index.Build(fs.Arg(0), index.BuildOptions{Rebuild: *rebuild})
	if err != nil {
		if errors.Is(err, index.ErrInvalidRepository) && res != nil && res.Validation != nil {
			if *asJSON {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(res.Validation); err != nil {
					fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
					return 2
				}
			} else {
				for _, issue := range res.Validation.Issues {
					fmt.Fprintf(stdout, "%-7s %-18s %s: %s\n", issue.Severity, issue.Code, issue.Path, issue.Message)
				}
				fmt.Fprintf(stdout, "\n%s: %d nodes, %d entries, %d errors, %d warnings; index left unchanged\n",
					res.Validation.Root, res.Validation.Nodes, res.Validation.Entries,
					res.Validation.Errors(), res.Validation.Warnings())
			}
			return 1
		}
		fmt.Fprintf(stderr, "memoryd: index failed: %v\n", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
		return 0
	}
	if res.UpToDate {
		fmt.Fprintf(stdout, "index is up to date: %s (generation %d, %d nodes, %d entries)\n",
			res.Path, res.Generation, res.Nodes, res.Entries)
	} else {
		fmt.Fprintf(stdout, "indexed %d nodes and %d entries into %s (generation %d)\n",
			res.Nodes, res.Entries, res.Path, res.Generation)
	}
	if res.Warnings > 0 {
		fmt.Fprintf(stdout, "%d validation warning(s); run memoryd validate for details\n", res.Warnings)
	}
	return 0
}

func runMCP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd mcp <data-dir>")
		return 2
	}
	dir := fs.Arg(0)

	res, err := index.Build(dir, index.BuildOptions{})
	if err != nil {
		if errors.Is(err, index.ErrInvalidRepository) && res != nil && res.Validation != nil {
			for _, issue := range res.Validation.Issues {
				fmt.Fprintf(stderr, "%-7s %-18s %s: %s\n", issue.Severity, issue.Code, issue.Path, issue.Message)
			}
			fmt.Fprintf(stderr, "\n%s: %d errors, %d warnings; MCP server not started\n",
				res.Validation.Root, res.Validation.Errors(), res.Validation.Warnings())
			return 1
		}
		fmt.Fprintf(stderr, "memoryd: cannot prepare index: %v\n", err)
		return 2
	}

	var warnings []string
	if res.Warnings > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"repository has %d validation warning(s); run memoryd validate %s for details", res.Warnings, dir))
	}
	ix, err := index.Open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot open index: %v\n", err)
		return 2
	}
	defer ix.Close()

	srv, err := mcpserver.New(ix, mcpserver.Options{Version: version, Warnings: warnings, Root: dir})
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot start MCP server: %v\n", err)
		return 2
	}
	if err := srv.Serve(stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "memoryd: MCP server stopped: %v\n", err)
		return 2
	}
	return 0
}

func runBench(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: memoryd bench [--json] <data-dir> <questions.jsonl>")
		return 2
	}
	questions, err := bench.LoadQuestions(fs.Arg(1))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot load questions: %v\n", err)
		return 2
	}
	results, err := bench.Run(fs.Arg(0), questions)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: bench failed: %v\n", err)
		return 2
	}
	firstHit, totalTokens := 0, 0
	for _, r := range results {
		totalTokens += r.Tokens
		if r.Rank == 1 {
			firstHit++
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{
			"results": results, "first_hit": firstHit,
			"total": len(results), "tokens_estimated": totalTokens,
		}); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
	} else {
		for _, r := range results {
			rank := "miss"
			if r.Rank > 0 {
				rank = strconv.Itoa(r.Rank)
			}
			fmt.Fprintf(stdout, "%4d  rank %-4s hits %-3d tokens %-6d %s\n", r.ID, rank, r.Hits, r.Tokens, r.Query)
		}
		fmt.Fprintf(stdout, "\nfirst-hit %d/%d, total tokens %d\n", firstHit, len(results), totalTokens)
	}
	if firstHit != len(results) {
		return 1
	}
	return 0
}

func runAdd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd add <data-dir> < entry.json")
		return 2
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot read input: %v\n", err)
		return 2
	}
	var in store.EntryInput
	if err := json.Unmarshal(data, &in); err != nil {
		fmt.Fprintf(stderr, "memoryd: invalid entry JSON: %v\n", err)
		return 2
	}
	res, err := store.AddEntry(fs.Arg(0), in)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: add failed: %v\n", err)
		if errors.Is(err, store.ErrWriteLocked) {
			fmt.Fprintln(stderr, "another write is in progress; retry shortly")
		}
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
		return 2
	}
	return 0
}

func runSupersede(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("supersede", flag.ContinueOnError)
	fs.SetOutput(stderr)
	oldID := fs.String("old", "", "entry UUIDv7 that becomes superseded")
	byID := fs.String("by", "", "entry UUIDv7 that supersedes it")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 || *oldID == "" || *byID == "" {
		fmt.Fprintln(stderr, "usage: memoryd supersede --old ID --by ID <data-dir>")
		return 2
	}
	res, err := store.SupersedeEntry(fs.Arg(0), *oldID, *byID)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: supersede failed: %v\n", err)
		if errors.Is(err, store.ErrWriteLocked) {
			fmt.Fprintln(stderr, "another write is in progress; retry shortly")
		}
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
		return 2
	}
	return 0
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd validate [--json] <data-dir>")
		return 2
	}

	res, err := validate.Validate(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: validate failed: %v\n", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
	} else {
		for _, issue := range res.Issues {
			fmt.Fprintf(stdout, "%-7s %-18s %s: %s\n", issue.Severity, issue.Code, issue.Path, issue.Message)
		}
		fmt.Fprintf(stdout, "\n%s: %d nodes, %d entries, %d errors, %d warnings\n",
			res.Root, res.Nodes, res.Entries, res.Errors(), res.Warnings())
	}

	if !res.OK() {
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `memoryd - MemoryTree command-line tool

Usage:
  memoryd init [--title TITLE] <data-dir>   create a new data repository
  memoryd validate [--json] <data-dir>      validate a data repository
  memoryd import-legacy --from DIR --to DATA-DIR [--agent NAME]
                                            import a flat memory directory
  memoryd index [--rebuild] [--json] <data-dir>
                                            build or refresh the search index
  memoryd mcp <data-dir>                    serve read-only MCP over stdio
  memoryd bench [--json] <data-dir> <questions.jsonl>
                                            rerun retrieval questions and
                                            check first hits
  memoryd add <data-dir> < entry.json       write one memory entry
  memoryd supersede --old ID --by ID <data-dir>
                                            link an existing supersede pair
  memoryd version                           print version information
  memoryd help                              show this help

Exit codes:
  0 success
  1 validation errors
  2 usage or I/O error
`)
}
