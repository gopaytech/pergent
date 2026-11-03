<!-- pergent -->
### ❌ 3 blocking issues

<sub>4 files · 3 blocking · 1 suggestion · 1 skill failed</sub>

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

> `code-review`: batch 2 was small

<details><summary>Files (4)</summary>

- `main.go` — reviewed by `code-review`
- `util.go` — reviewed by `code-review`
- `go.sum` — skipped by `code-review` (lockfile)
- `gen/<!--x.go` — skipped by `code-review` (generated file)

</details>

<!-- pergent:findings eyJ2ZXJzaW9uIjoxLCJza2lsbHMiOnsiY29kZS1yZXZpZXciOlt7ImZpbGUiOiJtYWluLmdvIiwibGluZSI6NDIsInNldmVyaXR5IjoiYmxvY2tpbmciLCJ0aXRsZSI6Ik5pbCBkZXJlZmVyZW5jZSIsImV4cGxhbmF0aW9uIjoicCBjYW4gYmUgbmlsIGhlcmUuIiwic3VnZ2VzdGVkX2ZpeCI6IkNoZWNrIHAgZmlyc3QuIiwiZmluZ2VycHJpbnQiOiJhYWFhYWFhYWFhYWEifSx7ImZpbGUiOiJ1dGlsLmdvIiwic2V2ZXJpdHkiOiJibG9ja2luZyIsInRpdGxlIjoiTGVhayIsImV4cGxhbmF0aW9uIjoiVGhlIGZpbGUgaXMgbmV2ZXIgY2xvc2VkLiIsImZpbmdlcnByaW50IjoiYmJiYmJiYmJiYmJiIn0seyJmaWxlIjoidXRpbC5nbyIsImxpbmUiOjcsInNldmVyaXR5Ijoic3VnZ2VzdGlvbiIsInRpdGxlIjoiUmVuYW1lIGhlbHBlciIsImV4cGxhbmF0aW9uIjoiVGhlIG5hbWUgaXMgdW5jbGVhci4iLCJzdWdnZXN0ZWRfZml4IjoiUmVuYW1lIGl0IHRvXG5gYGBnb1xuZm9vKClcbmBgYCIsImZpbmdlcnByaW50IjoiY2NjY2NjY2NjY2NjIn1dLCJkb2NzLXJldmlldyI6W10sIm9uZS1pc3N1ZSI6W3siZmlsZSI6InguZ28iLCJsaW5lIjoxLCJzZXZlcml0eSI6ImJsb2NraW5nIiwidGl0bGUiOiJCYWQiLCJleHBsYW5hdGlvbiI6IlJlYWxseSBiYWQuIn1dLCJzZWN1cml0eS1yZXZpZXciOltdfX0= -->
