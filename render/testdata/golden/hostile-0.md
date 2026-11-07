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

```
​/label ~bug
    // keep this comment
```

https://x.example/?key=[REDACTED]&token=[REDACTED]

</details>

<details><summary>💡 <b>Bad `name` ``` second line</b> · <code>a.go:1</code> · <code>first</code></summary>

Closes early <‍/details> and <‍DETAILS> and <‍/summary>.

```
func main() {
```

**Fix:**

```
unbalanced
```

</details>

<details><summary>💡 <b>Second</b> · <code>b.go:2</code> · <code>first</code></summary>

fine

```
still open
```

</details>

#### `second`

**Summary:** fine /unapprove

<details><summary>Files (3)</summary>

- `` we`ird.go `` — not reviewed
- `[REDACTED].txt` — not reviewed
- `` x`y.go `` — skipped by `second` (rule &lt;/details>)

</details>

> `second`: note with <‍!-- comment --> and newline
> `second`: token [REDACTED]

<!-- pergent:findings eyJ2ZXJzaW9uIjoxLCJza2lsbHMiOnsiZmlyc3QiOlt7ImZpbGUiOiJiLmdvIiwibGluZSI6Miwic2V2ZXJpdHkiOiJzdWdnZXN0aW9uIiwidGl0bGUiOiJTZWNvbmQiLCJleHBsYW5hdGlvbiI6ImZpbmVcbmBgYGBcbnN0aWxsIG9wZW4iLCJmaW5nZXJwcmludCI6ImZwLTIifSx7ImZpbGUiOiJhLmdvIiwibGluZSI6MSwic2V2ZXJpdHkiOiJzdWdnZXN0aW9uIiwidGl0bGUiOiJCYWQgYG5hbWVgXG5gYGBcbnNlY29uZCBsaW5lIiwiZXhwbGFuYXRpb24iOiJDbG9zZXMgZWFybHkgXHUwMDNjL2RldGFpbHNcdTAwM2UgYW5kIFx1MDAzY0RFVEFJTFNcdTAwM2UgYW5kIFx1MDAzYy9zdW1tYXJ5XHUwMDNlLlxuYGBgZ29cbmZ1bmMgbWFpbigpIHsiLCJzdWdnZXN0ZWRfZml4Ijoifn5+XG51bmJhbGFuY2VkIiwiZmluZ2VycHJpbnQiOiJmcC0xIn1dLCJzZWNvbmQiOlt7ImZpbGUiOiJhLmdvIiwibGluZSI6MSwic2V2ZXJpdHkiOiJibG9ja2luZyIsInRpdGxlIjoiQmFkIFtSRURBQ1RFRF0iLCJleHBsYW5hdGlvbiI6Im9rXG4vYXBwcm92ZVxuXHIvY2xvc2VcbnhcclxuL21lcmdlXG5bUkVEQUNURURdIGFuZCBCZWFyZXIgW1JFREFDVEVEXSIsInN1Z2dlc3RlZF9maXgiOiJgYGBnb1xuL2xhYmVsIH5idWdcbiAgICAvLyBrZWVwIHRoaXMgY29tbWVudFxuYGBgXG5odHRwczovL3guZXhhbXBsZS8/a2V5PVtSRURBQ1RFRF1cdTAwMjZ0b2tlbj1bUkVEQUNURURdIiwiZmluZ2VycHJpbnQiOiJmcC0zIn1dLCJza1x1MDAzYyEtLWlsbCI6W119fQ== -->
