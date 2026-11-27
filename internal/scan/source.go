package scan

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SourceLimits bound what one source archive may hold. Files counts every
// file, folder and link made. Unpacked counts file bytes and link targets.
// Download counts the bytes read from pergent.sh.
type SourceLimits struct {
	Files              int
	Unpacked, Download int64
}

// DefaultSourceLimits are the limits of a scan. The limit causes below name
// these numbers, and the command uses no other limits.
var DefaultSourceLimits = SourceLimits{200_000, 2 << 30, 500 << 20}

// The fixed causes of a refused archive. A cause never holds a name from
// the archive, so it is safe in a log and in the envelope.
const (
	causeNotArchive = "not a gzip tar archive"
	causeTopFolder  = "not one top folder"
	causeAbsolute   = "an absolute path"
	causeDotDot     = "a path with .."
	causeLinkLeaves = "a link that leaves the folder"
	causeHardLink   = "a hard link"
	causeDevice     = "a device file or FIFO"
	causeEntryType  = "an entry type pergent does not take"
	causePathTwice  = "a path twice"
	causeFiles      = "more than 200000 files"
	causeUnpacked   = "more than 2 GiB unpacked"
	causeDownload   = "more than 500 MiB downloaded"
	causeWrite      = "a file could not be written"
	causeDotGit     = "a .git path"
)

const (
	// sourceTries is how many times a download is tried. The waits come
	// before the second and the third try.
	sourceTries = 3
	// entrySlack is the tar stream allowed per entry on top of the file
	// bytes: the header, the padding and a short PAX record. It bounds the
	// work spent on an archive of huge headers.
	entrySlack = 4096
)

var sourceWaits = []time.Duration{2 * time.Second, 4 * time.Second}

// sourceClient never follows a redirect: the source comes from pergent.sh
// only, and the bearer token must not go anywhere else. It asks for no
// compression and decodes none, so a Content-Encoding: gzip header cannot
// turn the .tar.gz into a plain tar or move the download limit.
var sourceClient = &http.Client{
	Transport:     sourceTransport(),
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func sourceTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableCompression = true
	return t
}

// SourceError is why the source could not be used. Download is set for any
// download failure; else the archive broke an unpack rule or limit, and Cause
// is one of the fixed causes above.
type SourceError struct {
	Download bool
	Cause    string
}

func (e SourceError) Error() string {
	if e.Download {
		return "the source could not be downloaded"
	}
	return "the source archive was refused: " + e.Cause
}

func refused(cause string) error { return &SourceError{Cause: cause} }

// fetcher downloads the source. Tests replace wait and logf, so they do not
// sleep and can read the log.
type fetcher struct {
	client *http.Client
	waits  []time.Duration
	wait   func(ctx context.Context, d time.Duration) error
	logf   func(format string, args ...any)
}

func newFetcher() *fetcher {
	return &fetcher{client: sourceClient, waits: sourceWaits, wait: sleepCtx, logf: log.Printf}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// FetchSource downloads the source archive of one commit from url with the
// bearer token and unpacks it into dir, which must exist and be empty. A
// connection error or a 5xx is tried again, up to 3 tries; a redirect, a
// 4xx and any other code but 200 are final. The download stops when ctx
// ends. The error is a *SourceError, except when dir is not empty.
func FetchSource(ctx context.Context, url, token, dir string, lim SourceLimits) error {
	return newFetcher().fetch(ctx, url, token, dir, lim)
}

func (f *fetcher) fetch(ctx context.Context, url, token, dir string, lim SourceLimits) error {
	failed := &SourceError{Download: true}
	for try := 1; ; try++ {
		if try > 1 {
			if err := f.wait(ctx, f.waits[try-2]); err != nil {
				return failed
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			f.logf("source: the source URL is not valid")
			return failed
		}
		req.Header.Set("Authorization", "Bearer "+token)
		// The log never holds err: a net/http error names the whole URL.
		resp, err := f.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				f.logf("source: download stopped: out of time or cancelled")
				return failed
			}
			f.logf("source: download try %d of %d: connection error", try, sourceTries)
			if try < sourceTries {
				continue
			}
			return failed
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			f.logf("source: download try %d of %d: status %d", try, sourceTries, resp.StatusCode)
			if resp.StatusCode >= 500 && try < sourceTries {
				continue
			}
			return failed
		}
		err = f.unpack(resp.Body, dir, lim)
		resp.Body.Close()
		return err
	}
}

// unpack reads a 200 body. A read error of the body itself (cut short, out
// of time, over the download limit) is a download failure, whatever the tar
// reader made of it.
func (f *fetcher) unpack(body io.Reader, dir string, lim SourceLimits) error {
	cr := &countReader{r: body, left: lim.Download}
	err := Unpack(cr, dir, lim)
	switch {
	case cr.over:
		f.logf("source: download over the limit")
		return &SourceError{Download: true, Cause: causeDownload}
	case cr.err != nil:
		f.logf("source: download cut short")
		return &SourceError{Download: true}
	}
	return err
}

// countReader reads at most left bytes and keeps its first read error.
type countReader struct {
	r    io.Reader
	left int64
	over bool
	err  error
}

var errOverLimit = errors.New("over the limit")

func (c *countReader) Read(p []byte) (int, error) {
	if c.over {
		return 0, errOverLimit
	}
	if c.left <= 0 {
		// One byte more tells a body of exactly the limit from a longer one.
		var one [1]byte
		n, err := c.r.Read(one[:])
		if n > 0 {
			c.over = true
			return 0, errOverLimit
		}
		if err != nil && err != io.EOF && c.err == nil {
			c.err = err
		}
		return 0, err
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if err != nil && err != io.EOF && c.err == nil {
		c.err = err
	}
	return n, err
}

// Unpack reads a .tar.gz with one top folder (a GitLab archive or a GitHub
// tarball) from r into dir, which must exist and be empty. The archive is
// hostile: no write lands outside dir, nothing is written through a link,
// and the limits bound disk, memory and work. The top folder is stripped and
// pax global headers are skipped. Files get mode 0644, or 0755 when any
// execute bit is set; folders 0755; no owner, time or setuid is kept. On a
// refusal (a *SourceError) dir may hold part of the archive, but never a
// link that leaves it.
func Unpack(r io.Reader, dir string, lim SourceLimits) error {
	if err := emptyDir(dir); err != nil {
		return err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return refused(causeNotArchive)
	}
	u := &unpacker{
		dir:    dir,
		lim:    lim,
		root:   &node{kind: kindDir, kids: map[string]*node{}},
		stream: &countReader{r: gz, left: lim.Unpacked + int64(lim.Files)*entrySlack},
		buf:    make([]byte, 32<<10),
	}
	tr := tar.NewReader(u.stream)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		// With GODEBUG tarinsecurepath=0 the reader flags a bad name
		// itself; the checks below give it its fixed cause.
		if errors.Is(err, tar.ErrInsecurePath) && h != nil {
			err = nil
		}
		if err != nil {
			return u.readFailed()
		}
		if err := u.entry(h, tr); err != nil {
			return err
		}
	}
	if u.top == "" {
		return refused(causeTopFolder)
	}
	// Read to the gzip end, so its checksum is checked and data after it
	// is refused.
	if _, err := io.CopyBuffer(io.Discard, u.stream, u.buf); err != nil {
		return u.readFailed()
	}
	return nil
}

func emptyDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if _, err := d.Readdirnames(1); err != io.EOF {
		if err == nil {
			return errors.New("the source folder is not empty")
		}
		return err
	}
	return nil
}

type kind uint8

const (
	kindDir kind = iota
	kindFile
	kindLink
)

// node is one path the unpack made. The tree is the whole truth about dir,
// as dir started empty and nothing else writes to it meanwhile.
type node struct {
	kind     kind
	explicit bool // a folder that had its own entry
	kids     map[string]*node
}

type unpacker struct {
	dir    string
	lim    SourceLimits
	top    string
	root   *node
	count  int
	used   int64
	stream *countReader
	buf    []byte
}

func (u *unpacker) readFailed() error {
	if u.stream.over {
		return refused(causeUnpacked)
	}
	return refused(causeNotArchive)
}

func (u *unpacker) entry(h *tar.Header, tr *tar.Reader) error {
	if h.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	parts, err := u.split(h.Name)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		if h.Typeflag != tar.TypeDir {
			return refused(causeTopFolder)
		}
		return nil
	}
	switch h.Typeflag {
	case tar.TypeDir:
		return u.addDir(parts)
	case tar.TypeReg:
		return u.addFile(parts, h, tr)
	case tar.TypeSymlink:
		return u.addLink(parts, h.Linkname)
	case tar.TypeLink:
		return refused(causeHardLink)
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return refused(causeDevice)
	default:
		return refused(causeEntryType)
	}
}

// split checks an entry name and returns its parts below the top folder.
func (u *unpacker) split(name string) ([]string, error) {
	if strings.HasPrefix(name, "/") {
		return nil, refused(causeAbsolute)
	}
	var parts []string
	for _, p := range strings.Split(name, "/") {
		switch p {
		case "", ".":
			continue
		case "..":
			return nil, refused(causeDotDot)
		}
		if isDotGit(p) {
			return nil, refused(causeDotGit)
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return nil, refused(causeTopFolder)
	}
	if u.top == "" {
		u.top = parts[0]
	} else if parts[0] != u.top {
		return nil, refused(causeTopFolder)
	}
	return parts[1:], nil
}

// isDotGit reports whether a path part is .git, in any case. The source has
// no .git: the scrub skips every .git folder, ripgrep reads a root .git's
// ignore rules, and git would run with a .git's config.
func isDotGit(part string) bool { return strings.EqualFold(part, ".git") }

// parent returns the folder node of parts, making the folders it needs.
// A path under a link or a file is refused.
func (u *unpacker) parent(parts []string) (*node, error) {
	cur := u.root
	for i, p := range parts[:len(parts)-1] {
		next := cur.kids[p]
		if next == nil {
			if err := u.mkdir(parts[:i+1]); err != nil {
				return nil, err
			}
			next = &node{kind: kindDir, kids: map[string]*node{}}
			cur.kids[p] = next
		}
		switch next.kind {
		case kindLink:
			return nil, refused(causeLinkLeaves)
		case kindFile:
			return nil, refused(causePathTwice)
		}
		cur = next
	}
	return cur, nil
}

func (u *unpacker) path(parts []string) string {
	return filepath.Join(u.dir, filepath.Join(parts...))
}

// take counts one more path, and n more unpacked bytes.
func (u *unpacker) take(n int64) error {
	if u.count++; u.count > u.lim.Files {
		return refused(causeFiles)
	}
	if n > u.lim.Unpacked-u.used {
		return refused(causeUnpacked)
	}
	u.used += n
	return nil
}

func (u *unpacker) mkdir(parts []string) error {
	if err := u.take(0); err != nil {
		return err
	}
	p := u.path(parts)
	if err := os.Mkdir(p, 0o755); err != nil {
		return osRefusal(err)
	}
	// The umask must not change the mode.
	return osRefusal(os.Chmod(p, 0o755))
}

func (u *unpacker) addDir(parts []string) error {
	dir, err := u.parent(parts)
	if err != nil {
		return err
	}
	name := parts[len(parts)-1]
	if n := dir.kids[name]; n != nil {
		if n.kind != kindDir || n.explicit {
			return refused(causePathTwice)
		}
		n.explicit = true
		return nil
	}
	if err := u.mkdir(parts); err != nil {
		return err
	}
	dir.kids[name] = &node{kind: kindDir, explicit: true, kids: map[string]*node{}}
	return nil
}

func (u *unpacker) addFile(parts []string, h *tar.Header, tr *tar.Reader) error {
	dir, err := u.parent(parts)
	if err != nil {
		return err
	}
	name := parts[len(parts)-1]
	if dir.kids[name] != nil {
		return refused(causePathTwice)
	}
	if err := u.take(h.Size); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if h.Mode&0o111 != 0 {
		mode = 0o755
	}
	f, err := os.OpenFile(u.path(parts), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return osRefusal(err)
	}
	dir.kids[name] = &node{kind: kindFile}
	if err := u.copy(f, tr); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return osRefusal(err)
	}
	return osRefusal(f.Close())
}

// copy writes one file body. The tar reader gives exactly the header's
// size, which take already counted.
func (u *unpacker) copy(f *os.File, tr *tar.Reader) error {
	for {
		n, err := tr.Read(u.buf)
		if n > 0 {
			if _, werr := f.Write(u.buf[:n]); werr != nil {
				return refused(causeWrite)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return u.readFailed()
		}
	}
}

func (u *unpacker) addLink(parts []string, target string) error {
	dir, err := u.parent(parts)
	if err != nil {
		return err
	}
	name := parts[len(parts)-1]
	if dir.kids[name] != nil {
		return refused(causePathTwice)
	}
	if !u.linkInside(parts, target) {
		return refused(causeLinkLeaves)
	}
	for _, p := range strings.Split(target, "/") {
		if isDotGit(p) {
			return refused(causeDotGit)
		}
	}
	if err := u.take(int64(len(target))); err != nil {
		return err
	}
	if err := os.Symlink(target, u.path(parts)); err != nil {
		return osRefusal(err)
	}
	dir.kids[name] = &node{kind: kindLink}
	return nil
}

// linkInside walks a link target from the link's own folder. Each ".." must
// step out of a folder the archive already made: never out of dir, a link,
// a file or a name not seen yet. A folder can never turn into a link later,
// so the answer does not depend on the entry order. Other names only go
// down, and every link they pass through was checked the same way, so the
// target cannot resolve outside dir.
func (u *unpacker) linkInside(parts []string, target string) bool {
	if target == "" || strings.HasPrefix(target, "/") {
		return false
	}
	stack := []*node{u.root}
	for _, p := range parts[:len(parts)-1] {
		stack = append(stack, stack[len(stack)-1].kids[p])
	}
	for _, p := range strings.Split(target, "/") {
		top := stack[len(stack)-1]
		switch p {
		case "", ".":
		case "..":
			if len(stack) == 1 || top == nil || top.kind != kindDir {
				return false
			}
			stack = stack[:len(stack)-1]
		default:
			var next *node
			if top != nil && top.kind == kindDir {
				next = top.kids[p]
			}
			stack = append(stack, next)
		}
	}
	return true
}

// osRefusal turns a write error into a fixed cause. EEXIST and ENOTDIR come
// from two paths that differ only in case on a case-insensitive disk.
func osRefusal(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrExist), errors.Is(err, syscall.ENOTDIR):
		return refused(causePathTwice)
	}
	return refused(causeWrite)
}
