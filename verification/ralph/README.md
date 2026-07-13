# Bounded Ralph verification loop

The runner records and executes four explicit phases per iteration:

1. `plan`: resolve the seed, output paths, and implementation commands.
2. `do`: run Pitch and gorti independently.
3. `review`: compare canonical semantics and summarize performance.
4. `reflect`: complete on a match or retry up to `--max-iterations`.

`ralph.py` builds each command from an executable and a JSON array of arguments,
then expands placeholders without invoking a shell.

The supported entry point is `ralph.ps1`, which forwards arguments to the
cross-platform `ralph.py` engine. Commands are passed as an executable plus a
JSON array of arguments:

```powershell
.\verification\ralph\ralph.ps1 `
  --max-iterations 3 `
  --seed 42 `
  --output-dir .\artifacts\ralph-42 `
  --pitch-command python `
  --pitch-args-json '["scenario.py", "--rti", "pitch", "--log", "{log}"]' `
  --gorti-command python `
  --gorti-args-json '["scenario.py", "--rti", "gorti", "--log", "{log}"]'
```

`--pitch-arg` and `--gorti-arg` may be repeated instead. Use
`--pitch-arg=--verbose` when the argument begins with `-`. Available command
placeholders are `{seed}`, `{iteration}`, `{output_dir}`, `{run_dir}`, `{role}`,
and `{log}`. They are also exposed as `RALPH_*` environment variables.

With the default `--review-mode auto`, commands that create the canonical
`{log}` files are compared semantically using `verification/common`; otherwise
their captured stdout/stderr logs are compared byte-for-byte. Each command has
a timeout and is launched without a shell. A failed Pitch run does not prevent
the gorti run from being captured.

Every `iteration-NNN` directory records `plan.json`, process logs and metadata,
`review.json`, `reflect.json`, and a Markdown report. The output root gets a
final summary. Ralph returns `0` for a match, `1` when the bounded attempts are
exhausted, and `2` when PLAN cannot satisfy its dependencies. A non-empty
output directory is rejected to avoid replacing existing files.
