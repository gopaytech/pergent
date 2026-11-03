<!-- pergent -->
### ❌ 1 blocking issue

<sub>3 files · 1 blocking · 2 suggestions</sub>

| Skill | Verdict | Findings |
|---|---|---|
| `first` | ✅ No blocking issues | 2 suggestions |
| `second` | ❌ Has blocking issues | 1 blocking |
| `sk&lt;!--ill` | ✅ No blocking issues | none |

#### Findings (1)

<details open><summary>❌ <b>Bad [REDACTED]</b> · <code>a.go:1</code> · <code>second</code></summary>

ok
​/approve
​/close
x
​/merge
[REDACTED] and Bearer [REDACTED]

**Fix:**

```go
​/label ~bug
    // keep this comment
```
https://x.example/?key=[REDACTED]&token=[REDACTED]

</details>

> 2 suggestions omitted to fit the comment size limit.

> `second`: note with &lt;!-- comment --> and newline
> `second`: token [REDACTED]

<details><summary>Files (3)</summary>

- `` we`ird.go `` — not reviewed
- `[REDACTED].txt` — not reviewed
- `` x`y.go `` — skipped by `second` (rule &lt;/details>)

</details>

> Findings were too large to store for the next run; --previous-review will treat the next run as a first review.
