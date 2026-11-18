package main

import (
	"os"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
)

// writeUsageFile writes each skill's runs (result.EncodeUsage) to path, mode 0600.
// The file holds counts, tool names and target classes only: no paths and no
// model text, so local and CI mode write the same.
func writeUsageFile(path string, results []review.SkillResult) error {
	b, err := result.EncodeUsage(results)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
