package review

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/graphpack"
)

// graphMessage tells a batch run about the code graph hint. It names no tool:
// which tools a run has is opencode's business.
const graphMessage = "Also attached, as " + graphpack.FileName + ", are the callers and callees of the code this diff changes, and the files that import the changed files, from a code graph of the default branch. Use it to find related code with fewer searches. It can be out of date or miss code, so read the code before you rely on it."

// graphNote is the one note a skill gets when the hint could not be attached
// to some of its runs. It only helps the model, so those runs go on without
// it; the OS error stays in the log.
const graphNote = "The code graph hint could not be attached to every run."

// writeGraphPack writes the hint; a test replaces it to make that fail.
var writeGraphPack = os.WriteFile

// attachGraph builds the batch's hint and writes it to dir. path is "" when
// there is nothing to attach; failed is set when it could not be written,
// with a warning naming the skill and the batch.
func (r *reviewer) attachGraph(dir, skillName, label string, batch []diff.FileDiff) (path string, failed bool) {
	changes := make([]graphpack.Change, len(batch))
	for i, f := range batch {
		changes[i] = graphpack.Change{Path: f.Path, Patch: f.Patch}
		if f.OldPath != "" && !f.Copied {
			changes[i].OldPath = f.OldPath
		}
	}
	pack := r.graph.Build(changes, r.req.Sandbox.Hardened)
	if pack.Text == "" {
		return "", false
	}
	p := filepath.Join(dir, graphpack.FileName)
	if err := writeGraphPack(p, []byte(pack.Text), 0600); err != nil {
		warnf("graph: skipped for %s %s: %v", skillName, label, err)
		return "", true
	}
	return p, false
}

// noteGraphFailed adds graphNote unless the skill has it already.
func (res *SkillResult) noteGraphFailed() {
	if !slices.Contains(res.Notes, graphNote) {
		res.Notes = append(res.Notes, graphNote)
	}
}
