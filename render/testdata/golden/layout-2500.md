<!-- pergent -->
### ❌ 3 blocking issues

<sub>2 files · 3 blocking · 1 suggestion · 1 skill failed</sub>

| Skill | Verdict | Findings |
|---|---|---|
| `code-review` | ❌ Has blocking issues | 2 blocking · 1 suggestion |
| `security-review` | ✅ No blocking issues | none |
| `docs-review` | ⚠️ Failed | none |
| `one-issue` | ❌ Has blocking issues | 1 blocking |

⚠️ `docs-review`: Files `a.md`: **Review failed:** opencode exited normally after 3 steps, but the model did not produce a final answer.

⚠️ `docs-review`: Files `b.md`, `c.md`: **Review failed:** The model's answer was not in the required format.

#### Findings (4)

<details open><summary>❌ <b>Nil dereference</b> · <code>main.go:42</code> · <code>code-review</code></summary>

p can be nil here.

**Fix:** Check p first.

</details>

<details open><summary>❌ <b>Leak</b> · <code>util.go</code> · <code>code-review</code></summary>

The file is never closed.

</details>

<details open><summary>❌ <b>Bad</b> · <code>x.go:1</code> · <code>one-issue</code></summary>

Really bad.

</details>

<details><summary>💡 <b>Rename helper</b> · <code>util.go:7</code> · <code>code-review</code></summary>

The name is unclear.

**Fix:**

Rename it to
```go
foo()
```

</details>

<details><summary>Reviewed files (2)</summary>

- `main.go`
- `util.go`

</details>

<details><summary>Skipped files (2)</summary>

- `go.sum` — lockfile
- `gen/<!--x.go` — generated file

</details>

> batch 2 was small

<details><summary>Files changed (2)</summary>

- `main.go`
- `util.go`

</details>

> Findings were too large to store for the next run; --previous-review will treat the next run as a first review.
