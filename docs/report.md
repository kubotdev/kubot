# Report

`kubot report` prints the full inspection as one self-contained HTML page:
score header, severity-bucketed findings with evidence, a workload topology
diagram (ingress → service → workload, unhealthy nodes tinted), inventory
tables, and a health-history strip from stored runs.

```sh
kubot report -o report.html  # open in any browser
kubot report > report.html   # stdout works too, but -o
                             # writes UTF-8 directly: shell
                             # redirection (especially on
                             # Windows) may re-encode bytes
```

Inline CSS, one inline table sorter, zero external requests — safe to file,
mail, or attach to a ticket. Everything rendered is HTML-escaped, and every
non-ASCII character is emitted as an entity, so the file reads identically
under any text decoding (UTF-8 browsers, latin-1 mailers, OEM consoles);
a guard test fails the build on any external reference.

The history strip needs at least two stored runs in scope (see
[History](history.md)); with fewer, the section is omitted, never an error.
Like the terminal report, suppressed findings stay visible with their
reason. There is no `--json` — HTML is the artifact; agents parse
`inspect --json`.
