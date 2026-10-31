<!-- pergent -->
## pergent review

<details><summary>Files changed (2)</summary>

- `` we`ird.go ``
- `[REDACTED].txt`

</details>

<!-- pergent:first -->
### first

✅ **No blocking issues**

<details><summary>Suggestions (2)</summary>

**`b.go:2`** — **Second**

fine
````
still open
````

**`a.go:1`** — **Bad `name` ``` second line**

Closes early &lt;/details> and &lt;DETAILS> and &lt;/summary>.
```go
func main() {
```

_Suggested fix:_

~~~
unbalanced
~~~

</details>

<!-- /pergent:first -->

<!-- pergent:second -->
### second

❌ **1 blocking issue**

**Summary:** fine /unapprove

**`a.go:1`** — **Bad [REDACTED]**

ok
​/approve
​/close
x
​/merge
[REDACTED] and Bearer [REDACTED]

_Suggested fix:_

```go
​/label ~bug
    // keep this comment
```
https://x.example/?key=[REDACTED]&token=[REDACTED]

<details><summary>Skipped files (1)</summary>

- `` x`y.go `` — rule &lt;/details>

</details>

> note with &lt;!-- comment --> and newline
> token [REDACTED]

<!-- /pergent:second -->

<!-- pergent:sk<!--ill -->
### sk<!--ill

✅ **No blocking issues**

<!-- /pergent:sk<!--ill -->

<!-- pergent:findings eyJ2ZXJzaW9uIjoxLCJza2lsbHMiOnsiZmlyc3QiOlt7ImZpbGUiOiJiLmdvIiwibGluZSI6Miwic2V2ZXJpdHkiOiJzdWdnZXN0aW9uIiwidGl0bGUiOiJTZWNvbmQiLCJleHBsYW5hdGlvbiI6ImZpbmVcbmBgYGBcbnN0aWxsIG9wZW4iLCJmaW5nZXJwcmludCI6ImZwLTIifSx7ImZpbGUiOiJhLmdvIiwibGluZSI6MSwic2V2ZXJpdHkiOiJzdWdnZXN0aW9uIiwidGl0bGUiOiJCYWQgYG5hbWVgXG5gYGBcbnNlY29uZCBsaW5lIiwiZXhwbGFuYXRpb24iOiJDbG9zZXMgZWFybHkgXHUwMDNjL2RldGFpbHNcdTAwM2UgYW5kIFx1MDAzY0RFVEFJTFNcdTAwM2UgYW5kIFx1MDAzYy9zdW1tYXJ5XHUwMDNlLlxuYGBgZ29cbmZ1bmMgbWFpbigpIHsiLCJzdWdnZXN0ZWRfZml4Ijoifn5+XG51bmJhbGFuY2VkIiwiZmluZ2VycHJpbnQiOiJmcC0xIn1dLCJzZWNvbmQiOlt7ImZpbGUiOiJhLmdvIiwibGluZSI6MSwic2V2ZXJpdHkiOiJibG9ja2luZyIsInRpdGxlIjoiQmFkIFtSRURBQ1RFRF0iLCJleHBsYW5hdGlvbiI6Im9rXG4vYXBwcm92ZVxuXHIvY2xvc2VcbnhcclxuL21lcmdlXG5bUkVEQUNURURdIGFuZCBCZWFyZXIgW1JFREFDVEVEXSIsInN1Z2dlc3RlZF9maXgiOiJgYGBnb1xuL2xhYmVsIH5idWdcbiAgICAvLyBrZWVwIHRoaXMgY29tbWVudFxuYGBgXG5odHRwczovL3guZXhhbXBsZS8/a2V5PVtSRURBQ1RFRF1cdTAwMjZ0b2tlbj1bUkVEQUNURURdIiwiZmluZ2VycHJpbnQiOiJmcC0zIn1dLCJza1x1MDAzYyEtLWlsbCI6W119fQ== -->
