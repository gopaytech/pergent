package result

const (
	shortID = 6
	longID  = 8
)

// AssignIDs maps each distinct fingerprint to its short ID: the first 6
// characters. When two different fingerprints share those 6 characters,
// both get their first 8. IDs are only unique within the one result the
// fingerprints came from.
func AssignIDs(fps []string) map[string]string {
	distinct := make(map[string]struct{}, len(fps))
	for _, fp := range fps {
		distinct[fp] = struct{}{}
	}
	owners := make(map[string]int, len(distinct))
	for fp := range distinct {
		owners[prefix(fp, shortID)]++
	}
	ids := make(map[string]string, len(distinct))
	for fp := range distinct {
		n := shortID
		if owners[prefix(fp, shortID)] > 1 {
			n = longID
		}
		ids[fp] = prefix(fp, n)
	}
	return ids
}

func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
