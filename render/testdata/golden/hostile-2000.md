<!-- pergent -->
### ❌ 1 blocking issue

<sub>2 files · 1 blocking · 2 suggestions</sub>

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

Closes early &lt;/details> and &lt;DETAILS> and &lt;/summary>.
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

<details><summary>Skipped files (1)</summary>

- `` x`y.go `` — rule &lt;/details>

</details>

> note with &lt;!-- comment --> and newline
> token [REDACTED]

<details><summary>Files changed (2)</summary>

- `` we`ird.go ``
- `[REDACTED].txt`

</details>

> Findings were too large to store for the next run; --previous-review will treat the next run as a first review.
