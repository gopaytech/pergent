package result

const minID = 6

// AssignIDs maps each distinct fingerprint to its short ID. IDs start at
// the first 6 characters; while two distinct fingerprints share an ID, both
// are widened (8 first, then 9, 10, ... up to the full fingerprint). The
// same fingerprint twice is one entry and gets one ID. IDs are only unique
// within the one result the fingerprints came from.
func AssignIDs(fps []string) map[string]string {
	distinct := make([]string, 0, len(fps))
	seen := make(map[string]struct{}, len(fps))
	for _, fp := range fps {
		if _, ok := seen[fp]; !ok {
			seen[fp] = struct{}{}
			distinct = append(distinct, fp)
		}
	}

	width := make(map[string]int, len(distinct))
	for _, fp := range distinct {
		width[fp] = minID
	}
	for {
		owners := make(map[string][]string, len(distinct))
		for _, fp := range distinct {
			id := prefix(fp, width[fp])
			owners[id] = append(owners[id], fp)
		}
		widened := false
		for _, group := range owners {
			if len(group) < 2 {
				continue
			}
			for _, fp := range group {
				if width[fp] >= len(fp) {
					continue // already whole; cannot widen
				}
				if width[fp] == minID {
					width[fp] = 8
				} else {
					width[fp]++
				}
				widened = true
			}
		}
		if !widened {
			break
		}
	}

	ids := make(map[string]string, len(distinct))
	for _, fp := range distinct {
		ids[fp] = prefix(fp, width[fp])
	}
	return ids
}

func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
