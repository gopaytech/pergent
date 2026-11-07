package render

import (
	"fmt"
	"strings"
)

// The blocks a layout orders. Everything above them (the headline, the meta
// line, the reason, the skills table and the failures) and below them (the
// notes, the footer and the hidden findings block) is fixed.
const (
	BlockSummary   = "summary"
	BlockFindings  = "findings"
	BlockDismissed = "dismissed"
	BlockChecks    = "checks"
	BlockFiles     = "files"
)

// The two states of a block. A block a layout leaves out is not written.
const (
	StateOpen      = "open"
	StateCollapsed = "collapsed"
)

// Block is one entry of a layout: a block of the comment and whether it starts
// open or collapsed.
type Block struct {
	Name  string // one of the Block constants
	State string // StateOpen or StateCollapsed
}

// String returns the block as ParseLayout reads it, such as "summary:open".
func (b Block) String() string { return b.Name + ":" + b.State }

// DefaultLayout is the layout Comment uses when Options.Layout is empty or not
// valid.
func DefaultLayout() []Block {
	return []Block{
		{BlockSummary, StateOpen},
		{BlockFindings, StateCollapsed},
		{BlockDismissed, StateCollapsed},
		{BlockChecks, StateOpen},
		{BlockFiles, StateCollapsed},
	}
}

// ParseLayout reads a layout from entries of the form "block:state", such as
// "findings:collapsed". It refuses an empty list, an entry that is not of that
// form, an unknown block or state, a block named twice and a list without the
// findings block. An error names the bad entry.
func ParseLayout(entries []string) ([]Block, error) {
	blocks := make([]Block, 0, len(entries))
	for _, e := range entries {
		name, state, ok := strings.Cut(e, ":")
		if !ok {
			return nil, fmt.Errorf("layout entry %q: want block:state, such as %s", e, Block{BlockFindings, StateCollapsed})
		}
		blocks = append(blocks, Block{Name: name, State: state})
	}
	if err := checkLayout(blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// checkLayout holds the rules of a layout, for ParseLayout and Comment.
func checkLayout(blocks []Block) error {
	if len(blocks) == 0 {
		return fmt.Errorf("layout is empty")
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		switch b.Name {
		case BlockSummary, BlockFindings, BlockDismissed, BlockChecks, BlockFiles:
		default:
			return fmt.Errorf("layout entry %q: unknown block %q", b, b.Name)
		}
		switch b.State {
		case StateOpen, StateCollapsed:
		default:
			return fmt.Errorf("layout entry %q: unknown state %q, want %s or %s", b, b.State, StateOpen, StateCollapsed)
		}
		if seen[b.Name] {
			return fmt.Errorf("layout entry %q: block %q is listed twice", b, b.Name)
		}
		seen[b.Name] = true
	}
	if !seen[BlockFindings] {
		return fmt.Errorf("layout has no %q block; it is required", BlockFindings)
	}
	return nil
}

// resolveLayout returns the layout Comment draws: the caller's, or the default
// when it is empty or breaks a rule of checkLayout.
func resolveLayout(blocks []Block) []Block {
	if checkLayout(blocks) != nil {
		return DefaultLayout()
	}
	return blocks
}

// blockState returns the state of the named block and whether the layout has it.
func blockState(blocks []Block, name string) (state string, ok bool) {
	for _, b := range blocks {
		if b.Name == name {
			return b.State, true
		}
	}
	return "", false
}

// inDefaultOrder returns the blocks sorted as DefaultLayout orders them, with
// their states.
func inDefaultOrder(blocks []Block) []Block {
	var out []Block
	for _, d := range DefaultLayout() {
		if state, ok := blockState(blocks, d.Name); ok {
			out = append(out, Block{d.Name, state})
		}
	}
	return out
}
