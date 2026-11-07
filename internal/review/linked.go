package review

// LinkedRepo is a repository linked to the one under review, cloned by the
// caller before the review starts. Its content is as untrusted as the
// checkout's: pergent scrubs the clone, and only an impact run reads it.
type LinkedRepo struct {
	// Name is the link's name, as findings and the comment call it.
	Name string `json:"name"`
	// Path is the clone's directory. It is unused when Error is set.
	Path string `json:"path"`
	// Watched are the paths in the reviewed repository whose change makes this
	// link worth checking.
	Watched []string `json:"watched"`
	// CanBlock lets a confirmed finding from this link be blocking.
	CanBlock bool `json:"can_block"`
	// Error is why the link can't be used: the caller refused the entry or its
	// clone failed, or the scrub failed. The clone is then neither scrubbed nor
	// read.
	Error string `json:"error,omitempty"`
}
