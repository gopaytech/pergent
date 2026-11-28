package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/jsonschema"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/sarif"
	"github.com/gopaytech/pergent/internal/scan"
)

// scanDeps are the parts of `pergent scan` a test replaces. A nil field is
// the real one: os.Getenv, os.Stdout, os.Stderr, runner.Run,
// runner.ReadStats, a 30-second ticker and SIGTERM/SIGINT.
type scanDeps struct {
	Getenv         func(string) string
	Stdout, Stderr io.Writer
	Run            func(context.Context, runner.Options) runner.RunResult
	Stats          func(context.Context, string) (runner.Stats, error)
	Tick           <-chan time.Time
	Signals        <-chan os.Signal
}

const (
	defaultScanSkill   = "/opt/pergent/security-skills/security-audit"
	defaultScanTimeout = 3 * time.Hour
	minScanTimeout     = 10 * time.Minute
	maxScanTimeout     = 6 * time.Hour
	minScanSteps       = 5
	maxScanSteps       = 1000
	maxScanSubSteps    = 300
	// maxEntryBytes caps the skill's entry file, the agent prompt.
	maxEntryBytes = 1 << 20
	// maxSkillSchema caps the skill's report-schema.json, as the converter does.
	maxSkillSchema  = 1 << 20
	skillSchemaFile = "report-schema.json"
	// statsInterval is how often the usage is read while opencode runs.
	statsInterval = 30 * time.Second
)

// The stop reasons a scan's run context ends with. They are the envelope's
// reason texts too.
const (
	budgetReason = "token budget reached"
	signalReason = "cancelled"
)

// How long a stats read may take. Package vars so tests can shrink them.
var (
	// statsCallTimeout bounds one read while opencode runs.
	statsCallTimeout = 20 * time.Second
	// finalStatsTimeout bounds the last read after the run, a poll still
	// under way included.
	finalStatsTimeout = 60 * time.Second
	// signalStatsTimeout replaces it after a signal. The worst case after a
	// signal is about 35 seconds (opencode's 10-second wait, this read and a
	// 20-second delivery), so the Job's grace period must be at least 60
	// seconds, as pergent.sh sets it.
	signalStatsTimeout = 5 * time.Second
)

// The fixed causes of a scan that could not start. The envelope's reason is
// "the scan could not start: <cause>"; the log line before it has the detail.
const (
	causeInputs   = "the inputs are not valid"
	causeSkill    = "the skill is not usable"
	causeCheckout = "the checkout is not usable"
	causeScrub    = "the checkout could not be cleaned"
	causeOpencode = "opencode is not usable"
	causeWork     = "the work folder could not be made"
	causeConfig   = "the opencode config could not be made"
)

// scanInputs are the parsed flags and variables of `pergent scan`.
type scanInputs struct {
	repoPath, sourceURL, head, priorPath, skillDir string
	maxTurns, subTurns                             int
	// contextLimit and outputLimit are in tokens, 0 for the model's own.
	contextLimit, outputLimit int
	timeout                   time.Duration
	// budget is PERGENT_TOKEN_BUDGET, 0 for none.
	budget    int64
	keepProxy bool
	token     string
	target    result.Target
	// $HOME and $XDG_CONFIG_HOME, where opencode finds its global config.
	home, configHome string
}

// scanMain runs `pergent scan` and exits with its exit code.
func scanMain(args []string) {
	os.Exit(runScan(context.Background(), args, scanDeps{}))
}

// parseScan parses the flags. On a usage error (a non-empty message) the
// inputs still carry the delivery target, when one could be parsed; an HTTP
// target is only set with its token.
func parseScan(args []string, getenv func(string) string) (scanInputs, string) {
	var in scanInputs
	var resultPath, resultURL string
	flags := flag.NewFlagSet("pergent scan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&in.repoPath, "repo-path", "", "With --source-url, an empty folder pergent fills; without it, the top of a clone (required)")
	flags.StringVar(&in.sourceURL, "source-url", getenv("PERGENT_SCAN_SOURCE_URL"), "Download the source .tar.gz from here, with PERGENT_RESULT_TOKEN as the bearer token")
	flags.StringVar(&in.head, "head", "", "The commit SHA of the source; required with --source-url, else the clone's HEAD must equal it")
	flags.StringVar(&in.priorPath, "prior", getenv("PERGENT_SCAN_PRIOR"), "The prior scan's state and the dismissed findings, a JSON file outside --repo-path")
	flags.StringVar(&in.skillDir, "skill", defaultScanSkill, "The skill folder, with its pergent-scan.yaml, outside --repo-path")
	flags.IntVar(&in.maxTurns, "max-turns", 0, "The main agent's step limit, 5 to 1000 (default: the manifest's)")
	flags.IntVar(&in.subTurns, "max-subagent-turns", 0, "Each sub-agent's step limit, 5 to 300 (default: the manifest's)")
	flags.IntVar(&in.contextLimit, "context-limit", 0, "Model context window in tokens, only for models opencode has no limits for ($PERGENT_CONTEXT_LIMIT)")
	flags.IntVar(&in.outputLimit, "output-limit", 0, "Max output tokens of one model reply, thinking included, only for models opencode has no limits for; 64000 is enough for a scan ($PERGENT_OUTPUT_LIMIT)")
	flags.DurationVar(&in.timeout, "timeout", defaultScanTimeout, "Max wall-clock time of the scan, download included, 10m to 6h")
	flags.StringVar(&resultPath, "result", "", "Write the envelope JSON to this file, or - for stdout")
	flags.StringVar(&resultURL, "result-url", "", "POST the envelope JSON here, with PERGENT_RESULT_TOKEN as the bearer token")
	parseErr := flags.Parse(args)

	in.target = result.Target{Path: resultPath}
	in.token = getenv("PERGENT_RESULT_TOKEN")
	if resultURL != "" && in.token != "" {
		in.target.URL, in.target.Token = resultURL, in.token
	}
	usage := func(format string, a ...any) (scanInputs, string) { return in, fmt.Sprintf(format, a...) }

	switch {
	case parseErr != nil:
		if errors.Is(parseErr, flag.ErrHelp) {
			flags.SetOutput(os.Stderr)
			flags.PrintDefaults()
		}
		return usage("%v", parseErr)
	case flags.NArg() > 0:
		return usage("unexpected argument %q", flags.Arg(0))
	case resultPath == "" && resultURL == "":
		return usage("at least one of --result or --result-url is required")
	case resultURL != "" && in.token == "":
		return usage("--result-url requires PERGENT_RESULT_TOKEN")
	case in.repoPath == "":
		return usage("--repo-path is required")
	case in.head != "" && !commitSHA.MatchString(in.head):
		return usage("--head %q is not a full commit SHA (40 or 64 lowercase hex characters)", in.head)
	case in.sourceURL != "" && in.head == "":
		return usage("--source-url requires --head")
	case in.sourceURL != "" && in.token == "":
		return usage("--source-url requires PERGENT_RESULT_TOKEN")
	case in.maxTurns != 0 && (in.maxTurns < minScanSteps || in.maxTurns > maxScanSteps):
		return usage("--max-turns must be %d to %d, got %d", minScanSteps, maxScanSteps, in.maxTurns)
	case in.subTurns != 0 && (in.subTurns < minScanSteps || in.subTurns > maxScanSubSteps):
		return usage("--max-subagent-turns must be %d to %d, got %d", minScanSteps, maxScanSubSteps, in.subTurns)
	case in.timeout < minScanTimeout || in.timeout > maxScanTimeout:
		return usage("--timeout must be %s to %s, got %s", minScanTimeout, maxScanTimeout, in.timeout)
	case in.skillDir == "":
		return usage("--skill is empty")
	case getenv("OPENCODE_PROVIDER") == "" || getenv("OPENCODE_MODEL") == "":
		return usage("pergent scan requires OPENCODE_PROVIDER and OPENCODE_MODEL")
	}
	if v := getenv("PERGENT_TOKEN_BUDGET"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return usage("PERGENT_TOKEN_BUDGET must be a positive whole number, got %q", v)
		}
		in.budget = n
	}
	var err error
	if in.contextLimit, err = config.ResolveLimit(in.contextLimit, "PERGENT_CONTEXT_LIMIT", "context-limit", getenv); err != nil {
		return usage("%v", err)
	}
	if in.outputLimit, err = config.ResolveLimit(in.outputLimit, "PERGENT_OUTPUT_LIMIT", "output-limit", getenv); err != nil {
		return usage("%v", err)
	}
	if _, err := config.ExtraEnvNames(getenv(config.ExtraEnvVar)); err != nil {
		return usage("%v", err)
	}
	in.keepProxy = getenv("PERGENT_SCAN_KEEP_PROXY") == "true"
	in.home, in.configHome = getenv("HOME"), getenv("XDG_CONFIG_HOME")
	return in, ""
}

// scanner is one `pergent scan`: its inputs, what it learned so far, and
// the delivery state the signal handler reads.
type scanner struct {
	d      scanDeps
	in     scanInputs
	logger *log.Logger
	// commit and manifest fill the envelope of a scan that ends early.
	commit   string
	manifest scan.Manifest

	deliverCtx   context.Context
	stopDelivery context.CancelFunc
	// signalled is set and sigDone closed on the first signal.
	signalled  atomic.Bool
	sigDone    chan struct{}
	delivering atomic.Bool
	// statsLogged is set once a stats read failed: only the first is logged.
	statsLogged atomic.Bool
}

// runScan runs `pergent scan`: it gets the source, checks the skill, runs
// one opencode attempt and delivers the envelope. stdout carries nothing but
// the envelope (with --result -); logs go to stderr, redacted, and hold no
// model text. It returns the exit code: 0 delivered (any status), 2 usage,
// 3 no scan possible (a failed envelope delivered), 4 a delivery failed.
func runScan(ctx context.Context, args []string, d scanDeps) int {
	if len(args) > 0 && (args[0] == "--check-skill" || strings.HasPrefix(args[0], "--check-skill=")) {
		return checkSkillCommand(args, d)
	}
	if d.Getenv == nil {
		d.Getenv = os.Getenv
	}
	if d.Stdout == nil {
		d.Stdout = os.Stdout
	}
	if d.Stderr == nil {
		d.Stderr = os.Stderr
	}
	if d.Run == nil {
		d.Run = runner.Run
	}
	if d.Stats == nil {
		d.Stats = runner.ReadStats
	}
	if d.Signals == nil {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
		defer signal.Stop(sigs)
		d.Signals = sigs
	}
	if d.Tick == nil {
		t := time.NewTicker(statsInterval)
		defer t.Stop()
		d.Tick = t.C
	}

	s := &scanner{d: d, logger: log.New(runner.NewRedactedLineWriter(d.Stderr), "", 0), sigDone: make(chan struct{})}
	in, usageMsg := parseScan(args, d.Getenv)
	s.in = in
	if commitSHA.MatchString(in.head) {
		s.commit = in.head
	}

	// A signal stops the scan with the cause "cancelled". Delivery has its
	// own context, which the scan's end does not cancel.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	s.deliverCtx, s.stopDelivery = context.WithCancel(context.WithoutCancel(ctx))
	defer s.stopDelivery()
	go s.watchSignals(cancel)

	if usageMsg != "" {
		return s.usage(usageMsg)
	}
	return s.scan(ctx)
}

// watchSignals waits for the first signal, then stops the scan. A delivery
// already under way gets result.ShortDeadline to end; a later one is short
// by itself (send reads signalled).
func (s *scanner) watchSignals(cancel context.CancelCauseFunc) {
	select {
	case <-s.d.Signals:
	case <-s.deliverCtx.Done():
		return
	}
	s.logger.Printf("received a stop signal: stopping the scan and delivering what it has")
	s.signalled.Store(true)
	close(s.sigDone)
	cancel(stopSignal{signalReason})
	if s.delivering.Load() {
		time.AfterFunc(result.ShortDeadline, s.stopDelivery)
	}
}

// send delivers the envelope to every target.
func (s *scanner) send(body []byte) bool {
	// delivering is set before signalled is read, so a signal either makes
	// this delivery short or sees it and bounds it.
	s.delivering.Store(true)
	short := s.signalled.Load()
	if err := result.Deliver(s.deliverCtx, body, s.in.target, s.d.Stdout, short); err != nil {
		s.logger.Printf("delivering the envelope: %s", redact.String(err.Error()))
		return false
	}
	return true
}

// end builds the envelope of a scan that did not run opencode and delivers
// it when there is a target. It returns whether it was delivered.
func (s *scanner) end(in scan.EndInput) bool {
	if s.in.target == (result.Target{}) {
		return false
	}
	in.Commit, in.Manifest = s.commit, s.manifest
	in.Usage = scan.UsageFrom(nil, "", runner.Usage{})
	body, env := scan.Build(in)
	s.logger.Printf("scan: %s: %s", env.Status, env.Reason)
	return s.send(body)
}

// usage logs a usage error and sends a failed envelope. Always exit 2.
func (s *scanner) usage(msg string) int {
	s.logger.Printf("pergent scan: usage: %s", redact.String(msg))
	s.end(scan.EndInput{Stop: scan.StopSetup, SetupCause: causeInputs})
	return 2
}

// setupFailed logs why the scan could not start and sends a failed envelope.
// After a signal the setup step most likely failed because the signal
// stopped it, so the scan ends as cancelled instead.
func (s *scanner) setupFailed(cause, detail string) int {
	if s.signalled.Load() {
		s.logger.Printf("pergent scan: the signal stopped the setup (%s: %s)", cause, redact.String(detail))
		return s.cancelled()
	}
	s.logger.Printf("pergent scan: %s: %s", cause, redact.String(detail))
	if !s.end(scan.EndInput{Stop: scan.StopSetup, SetupCause: cause}) {
		return 4
	}
	return 3
}

// cancelled sends the envelope of a scan a signal stopped before opencode
// ran: partial, cancelled, with no results. The exit code is the signal
// path's: 0 delivered, 4 not.
func (s *scanner) cancelled() int {
	if !s.end(scan.EndInput{Stop: scan.StopSignal}) {
		return 4
	}
	return 0
}

// scan runs everything after the flags were parsed.
func (s *scanner) scan(ctx context.Context) int {
	start := time.Now()
	in := &s.in

	root, code, done := s.source(ctx)
	if done {
		return code
	}
	m, entry, err := loadScanSkill(in.skillDir)
	if err != nil {
		return s.setupFailed(causeSkill, err.Error())
	}
	s.manifest = m
	if in.maxTurns == 0 {
		in.maxTurns = m.Steps.Main
	}
	if in.subTurns == 0 {
		in.subTurns = m.Steps.Subagent
	}

	// The model reads the whole tree, so a nested AGENTS.md is an injection
	// path: instruction files always go, and so do the denied files.
	if e := handleInstructionFiles(root, true, s.logger); e != nil {
		return s.setupFailed(causeScrub, e.Message)
	}
	if e := deleteDeniedFiles(root, s.logger); e != nil {
		return s.setupFailed(causeScrub, e.Message)
	}
	warning, err := checkOpencodeVersion(ctx)
	if err != nil {
		return s.setupFailed(causeOpencode, err.Error())
	}
	if warning != "" {
		s.logger.Printf("warning: %s", warning)
	}

	// The output and prior folders are outside the checkout, under the
	// $TMPDIR/pergent-* folders the scan config lets the agents read. The
	// edit rule names the output folder only.
	work, err := os.MkdirTemp("", config.TempPrefix+"scan-")
	if err != nil {
		return s.setupFailed(causeWork, err.Error())
	}
	defer os.RemoveAll(work)
	outDir := filepath.Join(work, "out")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		return s.setupFailed(causeWork, err.Error())
	}
	priorRun := s.writePrior(m, filepath.Join(work, "prior"))
	dataDir, err := runner.NewDataDir()
	if err != nil {
		return s.setupFailed(causeWork, err.Error())
	}
	defer os.RemoveAll(dataDir)
	cfgPath, cleanup, err := config.GenerateScanConfig(config.ScanOptions{
		MaxSteps: in.maxTurns, SubagentSteps: in.subTurns,
		ContextLimit: in.contextLimit, OutputLimit: in.outputLimit, Prompt: string(entry),
		Tools: m.Tools, OutputDir: outDir, SkillDir: in.skillDir,
	})
	if err != nil {
		return s.setupFailed(causeConfig, err.Error())
	}
	defer cleanup()

	exclude := []string{"PERGENT_RESULT_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"}
	if !in.keepProxy {
		exclude = append(exclude, runner.ProxyEnvNames...)
	}
	opts := runner.Options{
		SkillName:  m.Name,
		ConfigPath: cfgPath,
		Message: m.TaskLine(scan.TaskValues{Skill: in.skillDir, Target: root, Commit: s.commit,
			Output: outDir, Prior: priorRun}),
		RepoPath: root,
		// The download counts against --timeout.
		Timeout:    max(in.timeout-time.Since(start), time.Second),
		StepLimit:  in.maxTurns,
		ExcludeEnv: exclude,
		DataDir:    dataDir,
		// A retry would start a 3-hour audit again.
		Once: true,
	}
	s.logger.Printf("scan: running %s on %s, step limit %d, within %s", m.Name, s.commit, in.maxTurns, opts.Timeout.Round(time.Second))
	res, stream, pending, last, budgetHit := s.run(ctx, opts, dataDir)

	stop, kind := scanStop(res, budgetHit)
	stats, source := s.finalStats(pending, last, dataDir)
	body, env := scan.Build(scan.EndInput{
		Commit:        s.commit,
		Stop:          stop,
		FailKind:      kind,
		OutputLimited: res.Cause != nil && res.Cause.OutputLimited(),
		Out:           scan.Convert(m, in.skillDir, outDir),
		State:         scan.ReadState(m, outDir),
		Usage:         scan.UsageFrom(stats, source, stream),
		Manifest:      m,
	})
	s.logger.Printf("scan: %s: %s; usage from %s, %d results", env.Status, env.Reason, env.Usage.Source, len(env.SARIF.Runs[0].Results))
	if !s.send(body) {
		return 4
	}
	s.logger.Print("Done.")
	return 0
}

// source finds the checkout root and the commit. With --source-url it fills
// the empty --repo-path from pergent.sh and calls no git. done means the
// scan ends with code.
func (s *scanner) source(ctx context.Context) (root string, code int, done bool) {
	in := &s.in
	if in.sourceURL == "" {
		r, e := checkoutRoot(in.repoPath)
		if e != nil {
			return "", s.setupFailed(causeCheckout, e.Message), true
		}
		if msg := s.trustedOutside(r); msg != "" {
			return "", s.usage(msg), true
		}
		got, err := git(r, "rev-parse", "HEAD")
		if err != nil {
			return "", s.setupFailed(causeCheckout, err.Error()), true
		}
		got = strings.TrimSpace(got)
		if in.head != "" && got != in.head {
			return "", s.usage(fmt.Sprintf("the clone's HEAD is %s, not --head %s", got, in.head)), true
		}
		s.commit = got
		return r, 0, false
	}

	r, msg := emptyFolder(in.repoPath)
	if msg != "" {
		return "", s.usage(msg), true
	}
	if msg := s.trustedOutside(r); msg != "" {
		return "", s.usage(msg), true
	}
	dl, stop := context.WithTimeout(ctx, in.timeout)
	err := scan.FetchSource(dl, in.sourceURL, in.token, r, scan.DefaultSourceLimits)
	stop()
	if err != nil {
		var se *scan.SourceError
		if !errors.As(err, &se) {
			return "", s.setupFailed(causeCheckout, err.Error()), true
		}
		if s.signalled.Load() {
			// signalled is set before the cancel, so a download the signal
			// stopped always sees it.
			s.logger.Printf("pergent scan: the signal stopped the download (%s)", se.Error())
			return "", s.cancelled(), true
		}
		s.logger.Printf("pergent scan: %s", se.Error())
		if !s.end(scan.EndInput{Stop: scan.StopSource, Source: se}) {
			return "", 4, true
		}
		return "", 3, true
	}
	s.logger.Printf("scan: source of %s unpacked", s.commit)
	return r, 0, false
}

// emptyFolder returns the real path of dir, which must be an existing,
// empty folder, or a usage message.
func emptyFolder(dir string) (string, string) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	var fi os.FileInfo
	if err == nil {
		fi, err = os.Stat(abs)
	}
	if err != nil || !fi.IsDir() {
		return "", fmt.Sprintf("--repo-path %s must be an existing empty folder with --source-url", dir)
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Sprintf("--repo-path %s: %v", dir, err)
	}
	defer f.Close()
	if _, err := f.Readdirnames(1); err != io.EOF {
		return "", fmt.Sprintf("--repo-path %s must be an existing empty folder with --source-url", dir)
	}
	return abs, ""
}

// trustedOutside refuses a trusted input inside the checkout, where the
// source could have put it: --skill, --prior, a --result file, $HOME and
// $XDG_CONFIG_HOME. It sets the skill folder to its real path. A --result
// file it refuses is dropped from the target, so nothing is written there.
func (s *scanner) trustedOutside(root string) string {
	in := &s.in
	if p := in.target.Path; p != "" && p != "-" {
		if e := resultPathError(root, p); e != nil {
			in.target.Path = ""
			return e.Message
		}
	}
	skill, err := resolvePath(in.skillDir)
	if err != nil {
		return fmt.Sprintf("--skill %s: %v", in.skillDir, err)
	}
	inputs := []trustedInput{{"--skill", skill}}
	if in.priorPath != "" {
		inputs = append(inputs, trustedInput{"--prior", in.priorPath})
	}
	if in.home != "" {
		inputs = append(inputs, trustedInput{"$HOME", in.home})
	}
	if in.configHome != "" {
		inputs = append(inputs, trustedInput{"$XDG_CONFIG_HOME", in.configHome})
	}
	for _, i := range inputs {
		resolved, err := resolvePath(i.path)
		if err != nil {
			return fmt.Sprintf("%s %s: %v", i.flag, i.path, err)
		}
		if within(root, resolved) {
			return fmt.Sprintf("%s %s is inside --repo-path: a scan never reads trusted inputs from the source", i.flag, i.path)
		}
	}
	in.skillDir = skill
	return ""
}

// loadScanSkill checks that the skill can be used and returns its manifest
// and its entry file, the agent prompt: the manifest passes, the entry is a
// regular file (read without following a link, at most maxEntryBytes), and
// a cloudflare-findings skill's report-schema.json compiles, since the
// converter fails every scan without it.
func loadScanSkill(dir string) (scan.Manifest, []byte, error) {
	m, err := scan.LoadManifest(dir)
	if err != nil {
		return scan.Manifest{}, nil, err
	}
	// ReadOutFile opens without following a link or blocking on a FIFO.
	entry, err := scan.ReadOutFile(dir, m.Entry, maxEntryBytes)
	if err != nil {
		return scan.Manifest{}, nil, fmt.Errorf("entry: %w", err)
	}
	if m.Output.Format == scan.FormatCloudflare {
		data, err := scan.ReadOutFile(dir, skillSchemaFile, maxSkillSchema)
		if err != nil {
			return scan.Manifest{}, nil, fmt.Errorf("%s: %w", skillSchemaFile, err)
		}
		if _, err := jsonschema.Compile(data); err != nil {
			return scan.Manifest{}, nil, fmt.Errorf("%s: %w", skillSchemaFile, err)
		}
	}
	return m, entry, nil
}

// checkSkillCommand is `pergent scan --check-skill <dir>`, the image's build
// check. It stands alone: no other flag, no source and no model. It returns
// 0 when the skill folder is usable and 1 when it is not.
func checkSkillCommand(args []string, d scanDeps) int {
	if d.Stdout == nil {
		d.Stdout = os.Stdout
	}
	if d.Stderr == nil {
		d.Stderr = os.Stderr
	}
	dir := ""
	switch {
	case len(args) == 2 && args[0] == "--check-skill":
		dir = args[1]
	case len(args) == 1 && strings.HasPrefix(args[0], "--check-skill="):
		dir = strings.TrimPrefix(args[0], "--check-skill=")
	default:
		fmt.Fprintln(d.Stderr, "pergent scan: --check-skill takes one folder and must be used alone")
		return 1
	}
	if err := checkSkill(dir); err != nil {
		fmt.Fprintf(d.Stderr, "pergent scan: skill check failed: %v\n", redact.String(err.Error()))
		return 1
	}
	fmt.Fprintf(d.Stdout, "skill ok: %s\n", dir)
	return 0
}

// checkSkill checks a skill folder as the image ships it: the manifest and
// the entry file pass (loadScanSkill, which also compiles the skill's
// report-schema.json for a cloudflare-findings skill), the schema and the
// converter agree on pergent's sample record (scan.CheckSample), LICENSE is
// a regular file, and the embedded SARIF schema compiles.
func checkSkill(dir string) error {
	m, _, err := loadScanSkill(dir)
	if err != nil {
		return err
	}
	if m.Output.Format == scan.FormatCloudflare {
		if err := scan.CheckSample(dir); err != nil {
			return err
		}
	}
	// Lstat: a link is not a licence file.
	fi, err := os.Lstat(filepath.Join(dir, "LICENSE"))
	if err != nil {
		return fmt.Errorf("LICENSE: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("LICENSE is not a regular file")
	}
	// An empty log is valid, so a problem here means the schema itself fails.
	empty, err := json.Marshal(sarif.Empty(sarif.Driver{Name: "check"}))
	if err != nil {
		return err
	}
	if problems := sarif.Validate(empty); len(problems) > 0 {
		return fmt.Errorf("the SARIF schema: %s", problems[0])
	}
	return nil
}

// writePrior reads --prior and writes the folder the skill reads. Any
// failure is a warning: the scan runs with no prior. It returns the folder,
// or "".
func (s *scanner) writePrior(m scan.Manifest, dir string) string {
	if s.in.priorPath == "" {
		return ""
	}
	p, err := scan.ReadPrior(s.in.priorPath)
	if err != nil {
		s.logger.Printf("warning: prior: %s; the scan runs with no prior", redact.String(err.Error()))
		return ""
	}
	run, err := scan.WritePrior(p, m, s.in.skillDir, dir)
	if err != nil {
		os.RemoveAll(dir)
		s.logger.Printf("warning: prior: writing it: %s; the scan runs with no prior", redact.String(err.Error()))
		return ""
	}
	return run
}

// statsReply is the answer of one stats read.
type statsReply struct {
	stats runner.Stats
	err   error
}

// run runs opencode once and reads the usage on every tick, one read at a
// time. Past the token budget it stops the run with budgetReason. It
// returns the run's result, the stream's usage, the reply channel of a read
// still under way (nil when none) and the last stats read that worked.
func (s *scanner) run(ctx context.Context, opts runner.Options, dataDir string) (
	res runner.RunResult, stream runner.Usage, pending chan statsReply, last *runner.Stats, budgetHit bool) {
	var mu sync.Mutex
	opts.OnUsage = func(u runner.Usage) {
		mu.Lock()
		stream = u
		mu.Unlock()
	}
	streamNow := func() runner.Usage {
		mu.Lock()
		defer mu.Unlock()
		return stream
	}
	runCtx, stopRun := context.WithCancelCause(ctx)
	defer stopRun(nil)
	done := make(chan runner.RunResult, 1)
	go func() { done <- s.d.Run(runCtx, opts) }()

	for {
		select {
		case res = <-done:
			return res, streamNow(), pending, last, budgetHit
		case <-s.d.Tick:
			if pending != nil || budgetHit {
				continue // reads never overlap
			}
			pending = s.readStats(ctx, statsCallTimeout, dataDir)
		case r := <-pending:
			pending = nil
			if r.err != nil {
				s.statsFailed(r.err)
			} else {
				last = &r.stats
			}
			if s.overBudget(last, streamNow()) && runCtx.Err() == nil {
				s.logger.Printf("scan: the token budget of %d is reached: stopping opencode", s.in.budget)
				budgetHit = true
				stopRun(stopSignal{budgetReason})
			}
		}
	}
}

// readStats starts one stats read bounded by d and returns its reply
// channel. A signal does not cut it: its own deadline does.
func (s *scanner) readStats(ctx context.Context, d time.Duration, dataDir string) chan statsReply {
	ch := make(chan statsReply, 1)
	go func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), d)
		defer cancel()
		st, err := s.d.Stats(c, dataDir)
		ch <- statsReply{st, err}
	}()
	return ch
}

// statsFailed logs the first stats failure of the scan.
func (s *scanner) statsFailed(err error) {
	if !s.statsLogged.Swap(true) {
		s.logger.Printf("warning: reading opencode stats: %s; usage counts the main session until a read works",
			redact.String(err.Error()))
	}
}

// overBudget reports whether the 5-part token total, the higher of the
// stats and the stream, reached the budget.
func (s *scanner) overBudget(last *runner.Stats, stream runner.Usage) bool {
	if s.in.budget == 0 {
		return false
	}
	total := tokenTotal(stream)
	if last != nil {
		total = max(total, tokenTotal(last.Tokens))
	}
	return int64(total) >= s.in.budget
}

func tokenTotal(u runner.Usage) int {
	return u.Input + u.Output + u.Reasoning + u.CacheRead + u.CacheWrite
}

// scanStop maps the end of the run to the envelope's Stop.
func scanStop(res runner.RunResult, budgetHit bool) (scan.Stop, runner.CauseKind) {
	if res.Status == runner.StatusCompleted {
		return scan.StopNone, ""
	}
	if res.Cause == nil {
		return scan.StopFailed, runner.CauseExitError
	}
	switch res.Cause.Kind {
	case runner.CauseCancelled:
		// The budget stops the run's own context; a signal stops the scan's.
		// The first one to end the run decides.
		if budgetHit && res.Cause.StopReason == budgetReason {
			return scan.StopBudget, ""
		}
		return scan.StopSignal, ""
	case runner.CauseTimeout:
		return scan.StopTimeout, ""
	case runner.CauseStepLimit:
		return scan.StopSteps, ""
	}
	return scan.StopFailed, res.Cause.Kind
}

// finalStats reads the usage once more after the run: a read still under
// way first, then a new one, both within finalStatsTimeout, or
// signalStatsTimeout once a signal came. The source is "stats" when the new
// read worked, "stats_poll" when only an earlier one did, and "" with nil
// stats when none did.
func (s *scanner) finalStats(pending chan statsReply, last *runner.Stats, dataDir string) (*runner.Stats, string) {
	limit := finalStatsTimeout
	if s.signalled.Load() {
		limit = min(limit, signalStatsTimeout)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline := time.Now().Add(limit)
	timer := time.AfterFunc(limit, cancel)
	defer timer.Stop()
	sig := s.sigDone
	wait := func(ch chan statsReply) (statsReply, bool) {
		for {
			select {
			case r := <-ch:
				return r, true
			case <-ctx.Done():
				return statsReply{}, false
			case <-sig:
				sig = nil
				if left := time.Until(deadline); left > signalStatsTimeout {
					deadline = time.Now().Add(signalStatsTimeout)
					timer.Reset(signalStatsTimeout)
				}
			}
		}
	}
	if pending != nil {
		if r, ok := wait(pending); ok && r.err == nil {
			last = &r.stats
		} else if ok {
			s.statsFailed(r.err)
		}
	}
	if ctx.Err() == nil {
		ch := make(chan statsReply, 1)
		go func() {
			st, err := s.d.Stats(ctx, dataDir)
			ch <- statsReply{st, err}
		}()
		r, ok := wait(ch)
		if ok && r.err == nil {
			return &r.stats, "stats"
		}
		if ok {
			s.statsFailed(r.err)
		}
	}
	if last != nil {
		return last, "stats_poll"
	}
	return nil, ""
}
