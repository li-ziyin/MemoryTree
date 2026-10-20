// Command memoryd is the MemoryTree command-line tool.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/li-ziyin/memorytree/internal/initrepo"
	"github.com/li-ziyin/memorytree/internal/legacy"
	"github.com/li-ziyin/memorytree/internal/validate"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
		fmt.Fprintln(stderr, "memoryd: index is not implemented yet; it is planned for milestone M1")
		return 3
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
  memoryd index <data-dir>                  build the search index (M1)
  memoryd version                           print version information
  memoryd help                              show this help

Exit codes:
  0 success
  1 validation errors
  2 usage or I/O error
  3 feature not implemented yet
`)
}
