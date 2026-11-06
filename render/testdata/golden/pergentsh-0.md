<!-- pergent -->
### ❌ 1 blocking issue · Confidence 3/5 (capped)

<sub>Commit `0123456` · 2 files · 1 blocking · 1 suggestion</sub>

> The token check is clear; the rest is small. (capped: 1 blocking finding)

#### Not on the diff (2)

<details open><summary>❌ <b>Expired tokens are accepted</b> · <a href="https://github.com/owner/repo/blob/0123456789abcdef/internal/auth/token.go?plain=1#L42-L44"><code>internal/auth/token.go:42–44</code></a></summary>

The expiry is compared with `>` where `<` is meant, so an expired token passes.

**Fix:** Reject the token when `exp` is before now.

_Suggested change, replacing lines 42–44:_

```
	if claims.Exp < now {
		return ErrExpired
	}
```

Not a real problem? Reply `@pergent-review dismiss a1b2c3 <reason>`

</details>

<details><summary>💡 <b>Document the token lifetime</b> · <a href="https://github.com/owner/repo/blob/0123456789abcdef/README.md?plain=1#L17"><code>README.md:17</code></a></summary>

_Line 17 is not part of the diff, so it could not be posted inline._

The README does not say how long a token is valid.

Not a real problem? Reply `@pergent-review dismiss d4e5f6 <reason>`

</details>

<details><summary>Files (2)</summary>

- `internal/auth/token.go`
- `README.md`

</details>

<sub>Run again: `@pergent-review review` · pergent v0.3.0</sub>

<!-- pergent:findings eyJ2ZXJzaW9uIjoxLCJza2lsbHMiOnsiY29kZS1yZXZpZXciOlt7ImZpbGUiOiJpbnRlcm5hbC9hdXRoL3Rva2VuLmdvIiwibGluZSI6NDIsImVuZF9saW5lIjo0NCwic2V2ZXJpdHkiOiJibG9ja2luZyIsInRpdGxlIjoiRXhwaXJlZCB0b2tlbnMgYXJlIGFjY2VwdGVkIiwiZXhwbGFuYXRpb24iOiJUaGUgZXhwaXJ5IGlzIGNvbXBhcmVkIHdpdGggYFx1MDAzZWAgd2hlcmUgYFx1MDAzY2AgaXMgbWVhbnQsIHNvIGFuIGV4cGlyZWQgdG9rZW4gcGFzc2VzLiIsInN1Z2dlc3RlZF9maXgiOiJSZWplY3QgdGhlIHRva2VuIHdoZW4gYGV4cGAgaXMgYmVmb3JlIG5vdy4iLCJzdWdnZXN0aW9uIjoiXHRpZiBjbGFpbXMuRXhwIFx1MDAzYyBub3cge1xuXHRcdHJldHVybiBFcnJFeHBpcmVkXG5cdH0iLCJmaW5nZXJwcmludCI6ImExYjJjM2Q0ZTVmNiJ9LHsiZmlsZSI6IlJFQURNRS5tZCIsImxpbmUiOjE3LCJzZXZlcml0eSI6InN1Z2dlc3Rpb24iLCJ0aXRsZSI6IkRvY3VtZW50IHRoZSB0b2tlbiBsaWZldGltZSIsImV4cGxhbmF0aW9uIjoiVGhlIFJFQURNRSBkb2VzIG5vdCBzYXkgaG93IGxvbmcgYSB0b2tlbiBpcyB2YWxpZC4iLCJmaW5nZXJwcmludCI6ImQ0ZTVmNmExYjJjMyJ9XX0sImNvbmZpZGVuY2UiOnsiY29kZS1yZXZpZXciOnsic2NvcmUiOjQsInJlYXNvbiI6IlRoZSB0b2tlbiBjaGVjayBpcyBjbGVhcjsgdGhlIHJlc3QgaXMgc21hbGwuIn19fQ== -->
