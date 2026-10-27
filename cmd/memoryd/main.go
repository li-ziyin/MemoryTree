// Command memoryd is the MemoryTree command-line tool.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/li-ziyin/memorytree/internal/bench"
	"github.com/li-ziyin/memorytree/internal/capture"
	"github.com/li-ziyin/memorytree/internal/clients"
	"github.com/li-ziyin/memorytree/internal/export"
	"github.com/li-ziyin/memorytree/internal/gardener"
	"github.com/li-ziyin/memorytree/internal/inbox"
	"github.com/li-ziyin/memorytree/internal/index"
	"github.com/li-ziyin/memorytree/internal/initrepo"
	"github.com/li-ziyin/memorytree/internal/legacy"
	"github.com/li-ziyin/memorytree/internal/mcpserver"
	"github.com/li-ziyin/memorytree/internal/model"
	"github.com/li-ziyin/memorytree/internal/mount"
	"github.com/li-ziyin/memorytree/internal/reposync"
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
	case "suggest-mount":
		return runSuggestMount(args[1:], stdin, stdout, stderr)
	case "supersede":
		return runSupersede(args[1:], stdout, stderr)
	case "inbox":
		return runInbox(args[1:], stdin, stdout, stderr)
	case "capture-session":
		return runCaptureSession(args[1:], stdin, stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "export":
		return runExport(args[1:], stdout, stderr)
	case "sync":
		return runSync(args[1:], stdout, stderr)
	case "install":
		return runInstall(args[1:], stdout, stderr)
	case "gardener":
		return runGardener(args[1:], stdin, stdout, stderr)
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

func runSuggestMount(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("suggest-mount", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	exclude := fs.String("exclude-entry", "", "entry UUIDv7 left out of scoring (leave-one-out evaluation)")
	limit := fs.Int("limit", 5, "number of candidates to return")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd suggest-mount [--json] [--exclude-entry ID] [--limit N] <data-dir> < entry.json")
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
	title := strings.TrimSpace(in.Title)
	if title == "" {
		fmt.Fprintln(stderr, "memoryd: title is required")
		return 2
	}
	switch in.Type {
	case "", model.TypeUser, model.TypeFeedback, model.TypeProject, model.TypeReference:
	default:
		fmt.Fprintln(stderr, "memoryd: type must be user, feedback, project, or reference")
		return 2
	}
	if *limit < 1 {
		fmt.Fprintln(stderr, "memoryd: --limit must be at least 1")
		return 2
	}

	res, snap, err := validate.ValidateWithSnapshot(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot read repository: %v\n", err)
		return 2
	}
	if !res.OK() {
		for _, issue := range res.Issues {
			fmt.Fprintf(stdout, "%-7s %-18s %s: %s\n", issue.Severity, issue.Code, issue.Path, issue.Message)
		}
		fmt.Fprintf(stderr, "memoryd: repository has %d validation errors\n", res.Errors())
		return 1
	}

	tags := make([]string, 0, len(in.TagsDelta))
	for _, tag := range in.TagsDelta {
		if tag = strings.ToLower(strings.TrimSpace(tag)); tag != "" {
			tags = append(tags, tag)
		}
	}
	if in.Type == "" {
		in.Type = model.TypeProject
	}
	suggestions := mount.Suggest(mount.FromSnapshot(snap, *exclude),
		mount.Query{Type: in.Type, Title: title, Summary: in.Summary, Tags: tags}, *limit)

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"suggestions": suggestions, "excluded": *exclude}); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
		return 0
	}
	for i, sugg := range suggestions {
		fmt.Fprintf(stdout, "%d. %-6.3f %s (%s)\n", i+1, sugg.Score, sugg.Path, sugg.Kind)
		for _, reason := range sugg.Reasons {
			fmt.Fprintf(stdout, "   - %s\n", reason)
		}
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

func runInbox(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "-", "input file, - for stdin")
	source := fs.String("source", "manual", "who captured the text")
	list := fs.Bool("list", false, "list stored captures")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd inbox [--file PATH] [--source NAME] [--list] <data-dir>")
		return 2
	}
	dir := fs.Arg(0)
	if *list {
		names, err := inbox.List(dir)
		if err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot list inbox: %v\n", err)
			return 2
		}
		for _, name := range names {
			fmt.Fprintln(stdout, name)
		}
		return 0
	}
	var data []byte
	var err error
	if *file == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot read input: %v\n", err)
		return 2
	}
	if len(data) == 0 {
		fmt.Fprintln(stderr, "memoryd: refusing to store an empty capture")
		return 2
	}
	path, redactions, err := inbox.Append(dir, *source, data)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: inbox failed: %v\n", err)
		return 2
	}
	if redactions > 0 {
		fmt.Fprintf(stdout, "redacted %d credential(s) before storing\n", redactions)
	}
	fmt.Fprintf(stdout, "stored %s\n", path)
	return 0
}

// runCaptureSession is the hook entry point: it merges the client's stdin
// payload with explicit flags, renders a bounded session summary, and
// stores it in the inbox (redacted there). Exit code 0 as long as the
// capture landed — a hook must never fail a client session.
func runCaptureSession(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("capture-session", flag.ContinueOnError)
	fs.SetOutput(stderr)
	source := fs.String("source", "hook", "client or person the session belongs to")
	session := fs.String("session", "", "session identifier")
	cwd := fs.String("cwd", "", "working directory of the session")
	event := fs.String("event", "", "hook event name")
	transcript := fs.String("transcript", "", "path to the session transcript (JSONL)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd capture-session [--source NAME] [--session ID] [--cwd DIR] [--event NAME] [--transcript PATH] <data-dir>")
		fmt.Fprintln(stderr, "       hook payload JSON on stdin is merged in (flags win)")
		return 2
	}

	// An interactive terminal is not a hook: treat it as no payload so the
	// command does not block waiting for EOF.
	reader := stdin
	if f, ok := stdin.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			reader = bytes.NewReader(nil)
		}
	}
	payload, _ := io.ReadAll(reader)
	var hook struct {
		SessionID      string `json:"session_id"`
		SessionIDCamel string `json:"sessionId"`
		Cwd            string `json:"cwd"`
		Event          string `json:"hook_event_name"`
		Transcript     string `json:"transcript_path"`
	}
	if len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, &hook); err != nil {
			fmt.Fprintf(stderr, "memoryd: hook payload is not JSON (%v); capturing from flags only\n", err)
		}
	}

	in := capture.Input{Source: *source}
	in.Session = firstNonEmpty(*session, hook.SessionID, hook.SessionIDCamel)
	in.Cwd = firstNonEmpty(*cwd, hook.Cwd)
	in.Event = firstNonEmpty(*event, hook.Event)
	in.TranscriptPath = firstNonEmpty(*transcript, hook.Transcript)

	content := capture.Build(in, time.Now().UTC())
	if len(content) == 0 {
		fmt.Fprintln(stderr, "memoryd: empty capture; nothing stored")
		return 2
	}
	path, redactions, err := inbox.AppendSession(fs.Arg(0), in.Source, in.Session, content)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: capture failed: %v\n", err)
		return 1
	}
	if redactions > 0 {
		fmt.Fprintf(stdout, "redacted %d item(s) before storing\n", redactions)
	}
	fmt.Fprintf(stdout, "captured session %s to %s\n", in.Session, path)
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func runGardener(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: memoryd gardener <data|apply> ...")
		return 2
	}
	switch args[0] {
	case "data":
		return runGardenerData(args[1:], stdout, stderr)
	case "apply":
		return runGardenerApply(args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "memoryd: unknown gardener subcommand %q\n", args[0])
		fmt.Fprintln(stderr, "usage: memoryd gardener <data|apply> ...")
		return 2
	}
}

func runGardenerData(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gardener data", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "-", "output file, - for stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd gardener data [--out PATH] <data-dir>")
		return 2
	}
	res, report, err := gardener.Data(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: gardener data failed: %v\n", err)
		return 2
	}
	if report == nil {
		for _, issue := range res.Issues {
			fmt.Fprintf(stdout, "%-7s %-18s %s: %s\n", issue.Severity, issue.Code, issue.Path, issue.Message)
		}
		fmt.Fprintf(stderr, "memoryd: repository has %d validation errors\n", res.Errors())
		return 1
	}
	var w = stdout
	if *out != "-" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot create %s: %v\n", *out, err)
			return 2
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot write report: %v\n", err)
		return 2
	}
	return 0
}

func runGardenerApply(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gardener apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "execute the proposal (default is dry-run)")
	asJSON := fs.Bool("json", false, "emit the full report as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd gardener apply [--apply] [--json] <data-dir> < proposal.json")
		return 2
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot read proposal: %v\n", err)
		return 2
	}
	var proposal gardener.Proposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		fmt.Fprintf(stderr, "memoryd: invalid proposal JSON: %v\n", err)
		return 2
	}

	res, err := store.ApplyGardenerProposal(fs.Arg(0), &proposal, store.GardenerOptions{DryRun: !*apply})
	if res != nil && res.Report != nil && *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
	}
	if err != nil {
		if errors.Is(err, store.ErrProposalRejected) {
			if res != nil && res.Report != nil && !*asJSON {
				printGardenerReport(stdout, res.Report)
			}
			fmt.Fprintln(stderr, "memoryd: proposal rejected; nothing was written")
			return 1
		}
		fmt.Fprintf(stderr, "memoryd: gardener apply failed: %v\n", err)
		return 1
	}
	if !*asJSON {
		printGardenerReport(stdout, res.Report)
		mode := "dry-run"
		if res.Committed {
			mode = fmt.Sprintf("applied and committed (generation %d, %d paths)", res.Generation, len(res.Changed))
		}
		fmt.Fprintf(stdout, "\n%s\n", mode)
	}
	return 0
}

func printGardenerReport(w io.Writer, report *gardener.Report) {
	for _, op := range report.Ops {
		status := op.Status
		if op.Code != "" {
			status = fmt.Sprintf("%s %s", op.Status, op.Code)
		}
		fmt.Fprintf(w, "%3d %-14s %-10s %s\n", op.Index, op.Op, status, op.Detail)
		if op.Message != "" {
			fmt.Fprintf(w, "      %s\n", op.Message)
		}
		for _, warn := range op.Warnings {
			fmt.Fprintf(w, "      warning: %s\n", warn)
		}
	}
	for _, diff := range report.TagDiffs {
		fmt.Fprintf(w, "tags %s: +%s -%s\n", diff.Path,
			strings.Join(diff.Added, ","), strings.Join(diff.Removed, ","))
	}
	for _, warn := range report.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fix := fs.Bool("fix", false, "remove crash leftovers and commit uncommitted managed files")
	asJSON := fs.Bool("json", false, "emit the report as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd doctor [--fix] [--json] <data-dir>")
		return 2
	}

	rep, err := store.Doctor(fs.Arg(0), *fix)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: doctor failed: %v\n", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
	} else {
		for _, rel := range rep.TempFiles {
			fmt.Fprintf(stdout, "temp   %s\n", rel)
		}
		for _, f := range rep.Dirty {
			fmt.Fprintf(stdout, "dirty  %-2s %s\n", f.Code, f.Path)
		}
		if rep.OtherUntracked > 0 {
			fmt.Fprintf(stdout, "note   %d untracked non-managed file(s) ignored\n", rep.OtherUntracked)
		}
		if rep.Validation != nil {
			for _, issue := range rep.Validation.Issues {
				fmt.Fprintf(stdout, "%-7s %-18s %s: %s\n", issue.Severity, issue.Code, issue.Path, issue.Message)
			}
		}
		if rep.Aborted != "" {
			fmt.Fprintf(stdout, "aborted: %s\n", rep.Aborted)
		}
		switch {
		case rep.Committed:
			fmt.Fprintf(stdout, "committed: %s\n", rep.CommitMessage)
		case rep.FixedTemp > 0 && len(rep.Dirty) == 0:
			fmt.Fprintf(stdout, "removed %d crash leftover(s)\n", rep.FixedTemp)
		case len(rep.Dirty) == 0 && len(rep.TempFiles) == 0:
			fmt.Fprintln(stdout, "healthy: no findings")
		default:
			fmt.Fprintf(stdout, "run with --fix to recover (nothing is discarded)\n")
		}
	}
	if rep.NeedsAttention() {
		return 1
	}
	return 0
}

func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	level := fs.String("level", "internal", "max sensitivity to include: public, internal, or private (private excludes nothing)")
	out := fs.String("out", "", "target directory for the export (must be empty or absent)")
	asJSON := fs.Bool("json", false, "emit the report as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 || *out == "" {
		fmt.Fprintln(stderr, "usage: memoryd export [--level public|internal|private] --out DIR <data-dir>")
		return 2
	}

	report, err := export.Export(fs.Arg(0), *out, *level)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: export failed: %v\n", err)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot write JSON: %v\n", err)
			return 2
		}
	} else {
		fmt.Fprintf(stdout, "exported %d nodes, %d entries to %s (level %s; skipped %d nodes, %d entries)\n",
			report.Nodes, report.Entries, report.Out, report.Level, report.NodesSkipped, report.EntriesSkipped)
		fmt.Fprintln(stdout, "share view only — see EXPORT-NOTES.md; do not validate or write against it")
	}
	return 0
}

func runSync(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd sync [--json] <data-dir>")
		return 2
	}
	res, err := reposync.Sync(fs.Arg(0))
	if err != nil {
		if errors.Is(err, reposync.ErrConflict) {
			if *asJSON {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(res)
			}
			fmt.Fprintf(stderr, "memoryd: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "memoryd: sync failed: %v\n", err)
		return 1
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
	switch {
	case res.UpToDate:
		fmt.Fprintf(stdout, "already up to date with %s (%s)\n", res.Upstream, res.Branch)
	default:
		verbs := []string{"fetched"}
		if res.Rebased {
			verbs = append(verbs, "rebased")
		}
		if res.Pushed {
			verbs = append(verbs, "pushed")
		}
		fmt.Fprintf(stdout, "%s %s (index generation %d)\n", strings.Join(verbs, ", "), res.Upstream, res.IndexGen)
	}
	return 0
}

func runInstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	client := fs.String("client", "", "client to configure: "+strings.Join(clients.Known(), ", "))
	hooks := fs.Bool("hooks", false, "install the session-end capture hook instead of the MCP entry (clients: "+strings.Join(clients.HookKnown(), ", ")+")")
	remove := fs.Bool("remove", false, "remove the memorytree entry (or hook) instead of adding it")
	exePath := fs.String("exe", "", "path to the memoryd executable (default: this binary)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *client == "" {
		fmt.Fprintln(stderr, "usage: memoryd install --client NAME [--exe PATH] <data-dir>")
		fmt.Fprintln(stderr, "       memoryd install --client NAME --remove")
		fmt.Fprintln(stderr, "       memoryd install --hooks --client NAME [--exe PATH] <data-dir>")
		fmt.Fprintln(stderr, "       memoryd install --hooks --client NAME --remove")
		return 2
	}
	known := false
	for _, name := range clients.Known() {
		if name == *client {
			known = true
			break
		}
	}
	if !known {
		fmt.Fprintf(stderr, "memoryd: unknown client %q; supported: %s\n", *client, strings.Join(clients.Known(), ", "))
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: cannot find home directory: %v\n", err)
		return 2
	}

	if *hooks {
		return runInstallHooks(fs, *client, *remove, *exePath, home, stdout, stderr)
	}

	if *remove {
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: memoryd install --client NAME --remove")
			return 2
		}
		path, removed, err := clients.Remove(home, *client)
		if err != nil {
			fmt.Fprintf(stderr, "memoryd: remove failed: %v\n", err)
			return 1
		}
		if !removed {
			fmt.Fprintf(stdout, "no memorytree entry found for %s\n", *client)
			return 0
		}
		fmt.Fprintf(stdout, "removed memorytree entry for %s: %s\nrestart %s (or its session) to unload the server\n", *client, path, *client)
		return 0
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd install --client NAME [--exe PATH] <data-dir>")
		return 2
	}
	dataDir, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: bad data directory: %v\n", err)
		return 2
	}
	exe := *exePath
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot locate this binary; pass --exe: %v\n", err)
			return 2
		}
	}
	path, err := clients.Install(home, *client, exe, dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: install failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "memorytree MCP entry written for %s: %s\nrestart %s (or its session) to load the server\n", *client, path, *client)
	return 0
}

// runInstallHooks installs or removes the session-end capture hook for the
// clients that support one; fs is the shared install flag set.
func runInstallHooks(fs *flag.FlagSet, client string, remove bool, exePath, home string, stdout, stderr io.Writer) int {
	if remove {
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: memoryd install --hooks --client NAME --remove")
			return 2
		}
		path, removed, err := clients.RemoveHooks(home, client)
		if err != nil {
			fmt.Fprintf(stderr, "memoryd: remove failed: %v\n", err)
			return 1
		}
		if !removed {
			fmt.Fprintf(stdout, "no memorytree capture hook found for %s\n", client)
			return 0
		}
		fmt.Fprintf(stdout, "removed session capture hook for %s: %s\n", client, path)
		return 0
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: memoryd install --hooks --client NAME [--exe PATH] <data-dir>")
		return 2
	}
	dataDir, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: bad data directory: %v\n", err)
		return 2
	}
	exe := exePath
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "memoryd: cannot locate this binary; pass --exe: %v\n", err)
			return 2
		}
	}
	path, err := clients.InstallHooks(home, client, exe, dataDir)
	if err != nil {
		fmt.Fprintf(stderr, "memoryd: install failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "session capture hook written for %s: %s\nfinished sessions will leave a redacted capture in %s (memoryd inbox)\n", client, path, filepath.Join(dataDir, ".inbox"))
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
  memoryd suggest-mount [--json] [--exclude-entry ID] [--limit N] <data-dir> < entry.json
                                            rank host nodes for a new entry
  memoryd supersede --old ID --by ID <data-dir>
                                            link an existing supersede pair
  memoryd inbox [--file PATH] [--source NAME] [--list] <data-dir>
                                            append a raw capture to .inbox/
  memoryd doctor [--fix] [--json] <data-dir>
                                            detect and recover crashed writes
  memoryd export [--level public|internal|private] --out DIR <data-dir>
                                            copy a sensitivity-filtered share view
  memoryd sync [--json] <data-dir>          fetch + rebase + push the data
                                            repository under the write lock
                                            (dirty trees refused; conflicts
                                            left for git to resolve)
  memoryd install --client NAME [--exe PATH] <data-dir>
                                            register memorytree MCP in a client
                                            (deepseek, opencode, qoder,
                                            workbuddy, zcode)
  memoryd install --client NAME --remove    remove the memorytree entry
  memoryd install --hooks --client NAME [--exe PATH] <data-dir>
                                            install the session-end capture
                                            hook (opencode, zcode)
  memoryd install --hooks --client NAME --remove
                                            remove the capture hook
  memoryd capture-session [--source NAME] [--session ID] [--cwd DIR] [--transcript PATH] <data-dir>
                                            store one session summary in the
                                            inbox (hook entry point; stdin
                                            payload merged)
  memoryd gardener data [--out PATH] <data-dir>
                                            emit the tree analysis package
  memoryd gardener apply [--apply] [--json] <data-dir> < proposal.json
                                            validate (default) or apply a
                                            tree-restructure proposal
  memoryd version                           print version information
  memoryd help                              show this help

Exit codes:
  0 success
  1 validation errors
  2 usage or I/O error
`)
}
