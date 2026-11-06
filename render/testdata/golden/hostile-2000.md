<!-- pergent -->
### ❌ 1 blocking issue

<sub>3 files · 1 blocking · 2 suggestions</sub>

| Skill | Verdict | Findings |
|---|---|---|
| `first` | ✅ No blocking issues | 2 suggestions |
| `second` | ❌ Has blocking issues | 1 blocking |
| `sk&lt;!--ill` | ✅ No blocking issues | none |

#### Findings (3)

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

<details><summary>💡 <b>Bad `name` ``` second line</b> · <code>a.go:1</code> · <code>first</code></summary>

Closes early <‍/details> and <‍DETAILS> and <‍/summary>.
```go
func main() {
```

**Fix:**

~~~
unbalanced
~~~

</details>

<details><summary>💡 <b>Second</b> · <code>b.go:2</code> · <code>first</code></summary>

fine
````
still open
````

</details>

> `second`: note with <‍!-- comment --> and newline
> `second`: token [REDACTED]

<details><summary>Files (3)</summary>

- `` we`ird.go `` — not reviewed
- `[REDACTED].txt` — not reviewed
- `` x`y.go `` — skipped by `second` (rule &lt;/details>)

</details>

> Findings were too large to store for the next run; --previous-review will treat the next run as a first review.
