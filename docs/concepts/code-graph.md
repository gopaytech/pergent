# 🕸️ Code graph

## What it is

A code graph lists the files, functions, methods and types of a repository, and who calls and imports what. pergent can read one and give each batch run a short hint: the callers and callees of the code the batch changes, and the files that import the changed files. The model then needs fewer `read` and `grep` steps to find related code.

It is **off by default**. A graph is a hint for the model, never a source of truth. Build it from the default branch.

## Build a graph

pergent does not build the graph. A provider does, and `pergent graph convert` turns its output into pergent's own format. The first provider is [graphify](https://pypi.org/project/graphifyy/). pergent reads the output of graphify **0.9.112** only. Any other version fails the convert with a clear error.

1. Install graphify 0.9.112 yourself, for example `pip install graphifyy==0.9.112` in a virtual environment.
2. Run it on your checkout with `--code-only`, so no LLM is used and no file leaves the machine:

   ```bash
   graphify extract <repo> --code-only
   ```

   This writes `<repo>/graphify-out/graph.json`. Do not pass `--no-cluster`: that writes graphify's raw output, which pergent does not read. With `--out <dir>`, the file is `<dir>/graphify-out/graph.json`.
3. Convert it:

   ```bash
   pergent graph convert --provider graphify \
     --in <repo>/graphify-out --out graph.json \
     --commit "$(git -C <repo> rev-parse HEAD)"
   ```

`--in` is the folder that holds graphify's `graph.json`. `--provider` is `graphify` by default.

`--commit` is the full commit SHA the graph was built from. graphify records the commit of the git repository that holds its output folder, and records none when that folder is not in a git repository. So pass `--commit` when the checkout has no `.git` or when `--out` is outside the checkout. Without it and without a recorded commit, the convert fails with `no commit: pass --commit`.

The convert reads no LLM key, no platform token and no opencode setting, and makes no network call. It writes the file with mode 0600 and prints one line with what it kept and dropped. The exit code is 0 when the file is written, 1 when the convert fails, and 2 for a usage error.

## What the graph holds

The format is `pergent-graph/v1`, one JSON file:

| Field | What it holds |
| --- | --- |
| `format` | `pergent-graph/v1` |
| `provider`, `provider_version` | `graphify`, `0.9.112` |
| `commit` | the full commit SHA the graph was built from |
| `built_at` | when the convert ran (RFC 3339) |
| `nodes` | `id`, `kind` (`file`, `function`, `method`, `type`, `config_key`), `name`, `file`, `line`, and `end_line` when the provider knows it |
| `edges` | `from`, `to`, `kind` (`calls`, `imports`, `inherits`, `contains`) |

The rules that keep it safe:

- **No free text.** A name is one identifier (a file is named by its base name). Comments, docstrings, string values, labels and every other graphify field are never copied. A graphify node whose name is not one identifier is dropped.
- **No denied file.** A node in a file that pergent's read rules deny is dropped, with every edge to it. That is `*.env` and `*.env.*` (not `*.env.example`), `*.pem`, `*.key` and `*.tfvars`, judged as written and in lower case.
- **Code files only.** In a file that is not code (YAML, JSON, Markdown, ...), only the file itself is kept. Config keys and document names there can hold data.
- **pergent's own ids.** Nodes are numbered `n1`, `n2`, ... in a fixed order, so no graphify id is copied, and the same input gives the same file.
- **Calls graphify is sure of, or nearly sure.** graphify marks each call `EXTRACTED`, `INFERRED` or `AMBIGUOUS`. pergent keeps the first two and drops `AMBIGUOUS`. graphify can still miss a call (a method called on a value, for example), so the graph never proves that code has no callers.

## Use it

```bash
pergent --local --skill code-review --graph graph.json
```

`--graph` works in local mode and in CI mode. Use it only when the change targets the default branch the graph was built from: on another branch the line numbers can be far off. A missing or bad file is skipped with one log line, `graph: skipped: <reason>`, and the review is the same as without the flag. A good file logs `graph: commit <sha>, <n> nodes, <m> edges`.

## The hint

Each batch run gets one more file, `graph-context.md`, last among its files, and one more sentence in its message. The file lists, for each changed file:

- the files that import it,
- for each symbol the change touches (found from the old-side lines of each hunk): its callers, and the functions it calls, as `name  file:line`. A method shows as `Type.Method`.

The rules:

- At most 10 callers and 10 callees per symbol, and about 2,000 tokens per file. Callers are kept first, then the importing files, then the callees, because the diff already shows part of what the changed code calls. When something is left out, the last line says how many entries were left out.
- A symbol with nothing to list is left out, and a batch with nothing to list gets no file. The hint never says that code has no callers, because the graph can miss a call.
- Entries in files the read rules deny are left out. In agent mode that is the hardened set.
- A renamed file is looked up under its old path, because the graph is from the default branch.
- Only batch runs get the hint. The repair, cross-batch, verification and impact runs do not.
- The hint never changes a verdict or the batches. If it cannot be written, the run goes on without it, a warning goes to the log, and the skill gets the note `The code graph hint could not be attached to every run.`

## Limits

A `pergent-graph/v1` file is at most 64 MiB, when it is written and when it is read. graphify's `graph.json` is read up to 512 MiB.
