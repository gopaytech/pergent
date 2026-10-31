<!-- pergent -->
## pergent review

<details><summary>Files changed (2)</summary>

- and 2 more not listed to fit the comment size limit

</details>

<!-- pergent:code-review -->
### code-review

❌ **2 blocking issues**

**`main.go:42`** — **Nil dereference**

p can be nil here.

_Suggested fix:_ Check p first.

**`util.go`** — **Leak**

The file is never closed.

> 1 suggestion omitted to fit the comment size limit.

<details><summary>Reviewed files (2)</summary>

- and 2 more not listed to fit the comment size limit

</details>

<details><summary>Skipped files (2)</summary>

- and 2 more not listed to fit the comment size limit


> Comment truncated to fit the comment size limit.
