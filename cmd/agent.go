package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/platform"
	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/skill"
)

// agentInputs is everything `pergent agent` reviews with. parseAgent fills
// the flags; prepareAgent loads the skills and fills the checkout-derived
// fields (Diff, Files, Classifier, Previous).
type agentInputs struct {
	RepoPath, Base, Head string
	Skills               []skill.Skill
	// Instructions are the absolute paths of the instruction files the agent
	// gets: --instructions, or none. Never discovered in the checkout.
	Instructions   []string
	Previous       map[string][]finding.Finding
	MaxCommentSize int
	Target         result.Target
	Diff           string
	Files          []string
	Classifier     diff.Classifier
	Review         config.Config

	// Flag values prepareAgent acts on.
	skillsDir    string
	skillValues  []string
	previousPath string
	scrub        bool
}

// ciOnlyFlags are CI-mode flags that agent mode refuses.
var ciOnlyFlags = map[string]bool{"local": true, "platform": true, "previous-review": true, "base-branch": true}

// commitSHA is what --base and --head must look like: a full SHA-1 or
// SHA-256 commit ID, as git prints it. It also keeps a value from being read
// as a git option.
var commitSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// minGitMinor is the oldest git 2.x agent mode runs on: GIT_ATTR_SOURCE,
// which makes the diff read attributes from the base commit, is 2.40.
const minGitMinor = 40

// instructionNames are the file names opencode loads as instructions from
// any directory it reads in, compared case-insensitively.
var instructionNames = []string{"AGENTS.md", "CLAUDE.md", "CONTEXT.md"}

// agentMain runs `pergent agent`. Running the review and delivering the
// result are not wired in yet.
func agentMain(args []string) {
	in, e := parseAgent(args, os.Getenv)
	if e != nil {
		fmt.Fprintf(os.Stderr, "pergent agent: %s\n", redact.String(e.Message))
		os.Exit(2)
	}
	for _, w := range in.Review.Warnings {
		log.Printf("warning: %s", w)
	}
	if e := prepareAgent(&in); e != nil {
		fmt.Fprintf(os.Stderr, "pergent agent: %s: %s\n", e.Code, redact.String(e.Message))
		os.Exit(3)
	}
	fmt.Fprintln(os.Stderr, "pergent agent: running the review is not implemented yet")
	os.Exit(1)
}

// parseAgent parses the agent-mode flags. On a usage error the returned
// inputs still carry whatever delivery target could be parsed, so an error
// result can be delivered; an HTTP target is only set with its token.
func parseAgent(args []string, getenv func(string) string) (agentInputs, *result.Error) {
	var in agentInputs
	var (
		skills                                  stringSlice
		resultPath, resultURL, instructions     string
		maxTurns, maxTokens, contextLimit, conc int
		timeout                                 time.Duration
		local, previousReview                   bool
		platformFlag, baseBranch                string
	)

	flags := flag.NewFlagSet("pergent agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&in.RepoPath, "repo-path", "", "Checkout of the head commit (required)")
	flags.StringVar(&in.Base, "base", "", "Merge-base commit SHA of the change (required)")
	flags.StringVar(&in.Head, "head", "", "Head commit SHA of the change; the checkout's HEAD must equal it (required)")
	flags.StringVar(&in.skillsDir, "skills-dir", "", "Directory of skills: *.md files and <name>/SKILL.md folders")
	flags.Var(&skills, "skill", "Preset name or path to skill .md file (repeatable)")
	flags.StringVar(&instructions, "instructions", "", "File passed to the agent as project instructions")
	flags.StringVar(&in.previousPath, "previous", "", "A previous agent result whose findings are attached per skill")
	flags.BoolVar(&in.scrub, "scrub-instructions", false, "Delete every AGENTS.md, CLAUDE.md and CONTEXT.md in --repo-path before reviewing")
	flags.IntVar(&in.MaxCommentSize, "max-comment-size", 0, "Byte limit for the rendered comment (0: no limit)")
	flags.StringVar(&resultPath, "result", "", "Write the result JSON to this file, or - for stdout")
	flags.StringVar(&resultURL, "result-url", "", "POST the result JSON here, with PERGENT_RESULT_TOKEN as the bearer token")
	flags.IntVar(&maxTurns, "max-turns", 0, "Max agentic turns per opencode run (default 20)")
	flags.IntVar(&maxTokens, "max-tokens", 0, "Deprecated and ignored; see --context-limit")
	flags.IntVar(&contextLimit, "context-limit", 0, "Model context window in tokens, only for models opencode has no limits for")
	flags.IntVar(&conc, "concurrency", 0, "Max opencode runs in flight at once (default 4)")
	flags.DurationVar(&timeout, "timeout", 0, "Max wall-clock time for the whole review (default 10m)")
	// CI-mode flags, defined only to refuse them by name.
	flags.BoolVar(&local, "local", false, "CI mode only")
	flags.StringVar(&platformFlag, "platform", "", "CI mode only")
	flags.BoolVar(&previousReview, "previous-review", false, "CI mode only")
	flags.StringVar(&baseBranch, "base-branch", "", "CI mode only")

	parseErr := flags.Parse(args)

	// The delivery target first, so even a usage error can be delivered.
	in.Target = result.Target{Path: resultPath}
	token := getenv("PERGENT_RESULT_TOKEN")
	if resultURL != "" && token != "" {
		in.Target.URL, in.Target.Token = resultURL, token
	}

	usage := func(format string, a ...any) (agentInputs, *result.Error) {
		return in, &result.Error{Code: result.CodeUsage, Message: fmt.Sprintf(format, a...)}
	}

	if parseErr != nil {
		if errors.Is(parseErr, flag.ErrHelp) {
			flags.SetOutput(os.Stderr)
			flags.PrintDefaults()
		}
		return usage("%v", parseErr)
	}
	if flags.NArg() > 0 {
		return usage("unexpected argument %q", flags.Arg(0))
	}
	var ciFlags []string
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) {
		set[f.Name] = true
		if ciOnlyFlags[f.Name] {
			ciFlags = append(ciFlags, "--"+f.Name)
		}
	})
	if len(ciFlags) > 0 {
		return usage("%s can't be used with pergent agent: CI mode only", strings.Join(ciFlags, ", "))
	}
	if resultPath == "" && resultURL == "" {
		return usage("at least one of --result or --result-url is required")
	}
	if resultURL != "" && token == "" {
		return usage("--result-url requires PERGENT_RESULT_TOKEN")
	}
	for _, f := range []struct{ name, value string }{{"repo-path", in.RepoPath}, {"base", in.Base}, {"head", in.Head}} {
		if f.value == "" {
			return usage("--%s is required", f.name)
		}
	}
	if !commitSHA.MatchString(in.Base) {
		return usage("--base %q is not a full commit SHA (40 or 64 lowercase hex characters)", in.Base)
	}
	if !commitSHA.MatchString(in.Head) {
		return usage("--head %q is not a full commit SHA (40 or 64 lowercase hex characters)", in.Head)
	}
	if in.skillsDir == "" && len(skills) == 0 {
		return usage("no skills: pass --skills-dir or --skill")
	}
	in.skillValues = skills
	if in.MaxCommentSize < 0 {
		return usage("--max-comment-size must be 0 or more, got %d", in.MaxCommentSize)
	}
	if getenv("OPENCODE_PROVIDER") == "" || getenv("OPENCODE_MODEL") == "" {
		return usage("agent mode requires OPENCODE_PROVIDER and OPENCODE_MODEL")
	}
	if instructions != "" {
		abs, err := filepath.Abs(instructions)
		if err != nil {
			return usage("--instructions %s: %v", instructions, err)
		}
		in.Instructions = []string{abs}
	}

	rc, err := config.ResolveAgent(config.Options{
		MaxTurns:       maxTurns,
		MaxTokens:      maxTokens,
		ContextLimit:   contextLimit,
		Concurrency:    conc,
		ConcurrencySet: set["concurrency"],
		Timeout:        timeout,
	})
	if err != nil {
		return usage("%v", err)
	}
	in.Review = rc
	return in, nil
}

// prepareAgent checks the inputs against the checkout and fills in what the
// review needs from it: the skills, the diff between the commits, the default
// skip rules from the base commit and the previous findings. It runs the
// --scrub-instructions deletion, or refuses a checkout with nested
// instruction files.
func prepareAgent(in *agentInputs) *result.Error {
	fail := func(code result.ErrorCode, format string, a ...any) *result.Error {
		return &result.Error{Code: code, Message: fmt.Sprintf(format, a...)}
	}

	out, err := exec.Command("git", "version").Output()
	if err != nil {
		return fail(result.CodeInternal, "running git version: %v", err)
	}
	if e := gitVersionError(string(out)); e != nil {
		return e
	}

	root, e := checkoutRoot(in.RepoPath)
	if e != nil {
		return e
	}
	if e := trustedInputsOutside(root, in); e != nil {
		return e
	}

	skills, e := loadAgentSkills(in.skillsDir, in.skillValues)
	if e != nil {
		return e
	}
	in.Skills = skills
	for _, path := range in.Instructions {
		f, err := os.Open(path)
		if err == nil {
			var info os.FileInfo
			info, err = f.Stat()
			f.Close()
			if err == nil && !info.Mode().IsRegular() {
				err = errors.New("not a regular file")
			}
		}
		if err != nil {
			return fail(result.CodeUsage, "--instructions %s: %v", path, err)
		}
	}

	for _, sha := range []string{in.Base, in.Head} {
		if _, err := git(root, "cat-file", "-e", sha+"^{commit}"); err != nil {
			return fail(result.CodeCommitMissing, "commit %s is not in the checkout", sha)
		}
	}
	got, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		// Both commits exist, so this is an unborn HEAD (an orphan branch with
		// no commit yet): the checkout is not at --head.
		return fail(result.CodeHeadMismatch, "the checkout's HEAD is not a commit, want --head %s", in.Head)
	}
	if got = strings.TrimSpace(got); got != in.Head {
		return fail(result.CodeHeadMismatch, "the checkout's HEAD is %s, not --head %s", got, in.Head)
	}

	if e := handleInstructionFiles(root, in.scrub); e != nil {
		return e
	}

	d, files, err := platform.CommitDiff(root, in.Base, in.Head)
	if err != nil {
		return fail(result.CodeDiffFailed, "%v", err)
	}
	in.Diff, in.Files = d, files

	in.Classifier = baseClassifier(root, in.Base)

	if in.previousPath != "" {
		in.Previous = readPrevious(in.previousPath)
	}
	return nil
}

// gitVersionError checks the output of `git version`: agent mode refuses a
// git older than 2.40, which ignores GIT_ATTR_SOURCE and so would let a
// change's own .gitattributes hide its files from the review.
func gitVersionError(out string) *result.Error {
	version, major, minor, err := platform.ParseGitVersion(out)
	if err != nil {
		return &result.Error{Code: result.CodeInternal, Message: err.Error()}
	}
	if major < 2 || (major == 2 && minor < minGitMinor) {
		return &result.Error{Code: result.CodeInternal, Message: fmt.Sprintf(
			"agent mode needs git 2.40 or newer (found %s) so default skip rules come from the base commit", version)}
	}
	return nil
}

// trustedInputsOutside refuses a trusted input (--instructions, --skills-dir,
// a path --skill, --previous) that resolves to somewhere inside the checkout:
// the change under review could have written it. A --skills-dir above the
// checkout is refused too, since LoadDir would read the checkout's top-level
// files as skills.
func trustedInputsOutside(root string, in *agentInputs) *result.Error {
	type input struct{ flag, path string }
	var inputs []input
	for _, p := range in.Instructions {
		inputs = append(inputs, input{"--instructions", p})
	}
	if in.skillsDir != "" {
		inputs = append(inputs, input{"--skills-dir", in.skillsDir})
	}
	for _, v := range in.skillValues {
		if skill.IsFilePath(v) {
			inputs = append(inputs, input{"--skill", v})
		}
	}
	if in.previousPath != "" {
		inputs = append(inputs, input{"--previous", in.previousPath})
	}
	for _, i := range inputs {
		resolved, err := resolvePath(i.path)
		if err != nil {
			return &result.Error{Code: result.CodeUsage, Message: fmt.Sprintf("%s %s: %v", i.flag, i.path, err)}
		}
		if within(root, resolved) || (i.flag == "--skills-dir" && within(resolved, root)) {
			return &result.Error{Code: result.CodeUsage, Message: fmt.Sprintf(
				"%s %s is inside --repo-path: agent mode never reads trusted inputs from the checkout", i.flag, i.path)}
		}
	}
	return nil
}

// resolvePath makes path absolute and follows symlinks in as much of it as
// exists, so a missing file under a symlinked directory still resolves.
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rest := ""
	for dir := abs; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest), nil
		}
		if filepath.Dir(dir) == dir {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// within reports whether path is dir or below it. Both must be clean and
// absolute.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// loadAgentSkills loads --skills-dir, then each --skill. Duplicate names are a
// usage error: the caller resolves library-versus-repo overrides.
func loadAgentSkills(dir string, values []string) ([]skill.Skill, *result.Error) {
	invalid := func(err error) *result.Error {
		return &result.Error{Code: result.CodeSkillsInvalid, Message: err.Error()}
	}
	var skills []skill.Skill
	if dir != "" {
		loaded, err := skill.LoadDir(dir)
		if err != nil {
			return nil, invalid(fmt.Errorf("--skills-dir %s: %w", dir, err))
		}
		skills = append(skills, loaded...)
	}
	for _, v := range values {
		s, err := skill.Resolve(v)
		if err != nil {
			return nil, invalid(fmt.Errorf("--skill %s: %w", v, err))
		}
		skills = append(skills, s)
	}
	if len(skills) == 0 {
		return nil, &result.Error{Code: result.CodeUsage, Message: "no skills: --skills-dir " + dir + " has none"}
	}
	if err := skill.CheckUnique(skills); err != nil {
		return nil, &result.Error{Code: result.CodeUsage, Message: err.Error()}
	}
	return skills, nil
}

// checkoutRoot returns repoPath with symlinks resolved, after checking it is
// the top of a git work tree. A subdirectory is refused: instruction files
// above it would escape the scrub, and diff paths are relative to the top.
func checkoutRoot(repoPath string) (string, *result.Error) {
	missing := &result.Error{Code: result.CodeCheckoutMissing, Message: fmt.Sprintf("--repo-path %s is not a git work tree", repoPath)}
	out, err := git(repoPath, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		// git's own reason, e.g. a missing directory or an ownership refusal.
		missing.Message += ": " + err.Error()
		return "", missing
	}
	if strings.TrimSpace(out) != "true" {
		return "", missing
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return "", missing
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", missing
	}
	top, err := git(repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", missing
	}
	topReal, err := filepath.EvalSymlinks(strings.TrimSpace(top))
	if err != nil || topReal != root {
		return "", &result.Error{Code: result.CodeCheckoutMissing, Message: fmt.Sprintf("--repo-path %s is not the top of its git work tree", repoPath)}
	}
	return root, nil
}

// handleInstructionFiles keeps opencode from loading instructions from the
// checkout. With scrub, every instruction file is deleted, the root's
// included. Without it, any instruction file below the root is refused, and
// so is any symlink to the root or above it: through "sub -> ." opencode
// would find the root's AGENTS.md as sub/AGENTS.md.
func handleInstructionFiles(root string, scrub bool) *result.Error {
	found, rootLinks, err := scanCheckout(root)
	if err != nil {
		return &result.Error{Code: result.CodeInternal, Message: fmt.Sprintf("looking for instruction files in the checkout: %v", err)}
	}
	if scrub {
		for _, rel := range found {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				return &result.Error{Code: result.CodeInternal, Message: fmt.Sprintf("--scrub-instructions: deleting %q: %v", rel, err)}
			}
			log.Printf("--scrub-instructions: deleted %q", rel)
		}
		return nil
	}
	var nested []string
	for _, rel := range found {
		if strings.Contains(rel, "/") {
			nested = append(nested, rel)
		}
	}
	var problems []string
	if len(nested) > 0 {
		problems = append(problems, fmt.Sprintf(
			"the checkout has instruction files below its root, which the agent would load: %s (pass --scrub-instructions to delete them)",
			quoteAll(nested)))
	}
	if len(rootLinks) > 0 {
		problems = append(problems, fmt.Sprintf(
			"the checkout has symlinks to its root or above it, through which the agent would load instruction files: %s",
			quoteAll(rootLinks)))
	}
	if len(problems) > 0 {
		return &result.Error{Code: result.CodeUntrustedInstructions, Message: strings.Join(problems, "; ")}
	}
	return nil
}

// scanCheckout walks root, skipping .git directories and not following
// symlinked directories. It returns, as sorted slash-separated paths relative
// to root, every non-directory entry named like an instruction file (a
// symlink so named included), and every symlink that resolves to root or an
// ancestor of it.
func scanCheckout(root string) (found, rootLinks []string, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if isInstructionName(d.Name()) {
			found = append(found, filepath.ToSlash(rel))
		}
		if d.Type()&fs.ModeSymlink != 0 {
			// A dangling or looping link reaches nothing and is ignored.
			if target, err := filepath.EvalSymlinks(path); err == nil && within(target, root) {
				rootLinks = append(rootLinks, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(found)
	sort.Strings(rootLinks)
	return found, rootLinks, err
}

// quoteAll formats names taken from the checkout with %q, comma-separated.
func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return strings.Join(quoted, ", ")
}

func isInstructionName(name string) bool {
	for _, n := range instructionNames {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

// baseClassifier builds the default skip rules from the base commit, never
// the working tree: .gitattributes as it was at base, and a generated-file
// header only when the base version of the file already had it.
func baseClassifier(root, base string) diff.Classifier {
	attrs, err := git(root, "show", base+":.gitattributes")
	if err != nil {
		attrs = ""
	}
	return diff.NewClassifierFrom([]byte(attrs), func(path string) bool {
		content, err := git(root, "show", base+":"+path)
		if err != nil {
			return false // not at base: a new file is reviewed
		}
		return diff.HasGeneratedHeader(content)
	})
}

// readPrevious reads a previous agent result's findings per skill. An
// unreadable file or one from another schema version is a first review.
func readPrevious(path string) map[string][]finding.Finding {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("warning: ignoring --previous: %v", err)
		return nil
	}
	var r result.Result
	if err := json.Unmarshal(data, &r); err != nil {
		log.Printf("warning: ignoring --previous %s: %v", path, err)
		return nil
	}
	if r.SchemaVersion != result.SchemaVersion {
		log.Printf("warning: ignoring --previous %s: schema_version %d, want %d", path, r.SchemaVersion, result.SchemaVersion)
		return nil
	}
	previous := make(map[string][]finding.Finding)
	for _, s := range r.Skills {
		for _, f := range s.Findings {
			previous[s.Name] = append(previous[s.Name], finding.Finding{
				File:         f.File,
				Line:         f.Line,
				Severity:     finding.Severity(f.Severity),
				Title:        f.Title,
				Explanation:  f.Explanation,
				SuggestedFix: f.SuggestedFix,
				Fingerprint:  f.Fingerprint,
			})
		}
	}
	return previous
}

// git runs git in repo with explicit arguments and returns its stdout. The
// error carries git's stderr.
func git(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}
