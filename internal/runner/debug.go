package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/gopaytech/pergent/internal/redact"
)

// debugSeq numbers the debug output files of this process.
var debugSeq atomic.Int64

// debugPath returns the file for debug output number n: base with ".<n>"
// before its extension, so /tmp/raw.json becomes /tmp/raw.3.json.
func debugPath(base string, n int64) string {
	ext := filepath.Ext(base)
	return fmt.Sprintf("%s.%d%s", strings.TrimSuffix(base, ext), n, ext)
}

// writeDebugOutput saves opencode's raw output for PERGENT_DEBUG_OUTPUT and
// returns the file it wrote. Every call writes a new file (runs go in
// parallel and each attempt is a run), readable by the user alone, with
// credentials redacted. A file that exists is never written over.
func writeDebugOutput(base string, raw []byte) (string, error) {
	data := []byte(redact.String(string(raw)))
	for {
		path := debugPath(base, debugSeq.Add(1))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return path, err
	}
}
