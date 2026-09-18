#!/usr/bin/env python3
"""Rewrite logger calls to their Context variants where a context is in scope.

slog.Logger.Info and friends log with context.Background() internally, so a
handler cannot read anything the request put in the context — which is where
the request and trace identity live. InfoContext and friends take the context
the caller already has.

What this does, per call site of the form `<expr>.log.Info(` or
`<expr>.logger.Info(`:

  1. The enclosing function has a named context parameter
     -> `Info(` becomes `InfoContext(<name>, `
  2. The enclosing function declares the context as `_`
     -> the parameter is renamed to ctx first, then as above
  3. The enclosing function has no context parameter
     -> left alone and reported, because the fix is a judgement: thread a
        context through the callers, or accept that this record has no
        correlation. A goroutine that outlives the request wants
        context.WithoutCancel rather than the request's own context.

Usage:
    python3 scripts/migrate-log-context.py [--dry-run] <file.go|dir> ...
"""

from __future__ import annotations

import pathlib
import re
import sys

FUNC = re.compile(
    r"^func\s+(\((?P<recv>[^)]*)\)\s+)?(?P<name>\w+)\((?P<params>[^{]*?)\)\s*(?P<ret>[^{]*?)\{",
    re.M | re.S,
)
CALL = re.compile(r"\w[\w.]*\.(?:logger|log)\.(?P<level>Debug|Info|Warn|Error)\(")
CTX_PARAM = re.compile(r"\b(\w+)\s+context\.Context")


class Site:
    def __init__(self, line: int, func: str) -> None:
        self.line = line
        self.func = func


def enclosing(funcs: list[tuple[int, re.Match[str]]], pos: int) -> re.Match[str] | None:
    found = None
    for start, match in funcs:
        if start <= pos:
            found = match
        else:
            break
    return found


def migrate(path: pathlib.Path, dry_run: bool) -> tuple[int, list[Site]]:
    src = path.read_text()
    funcs = sorted((m.start(), m) for m in FUNC.finditer(src))

    edits: list[tuple[int, int, str]] = []
    needs_ctx_name: dict[int, re.Match[str]] = {}
    deferred: list[Site] = []

    for call in CALL.finditer(src):
        func = enclosing(funcs, call.start())
        if func is None:
            deferred.append(Site(src[: call.start()].count("\n") + 1, "<file scope>"))
            continue
        ctx = CTX_PARAM.search(func.group("params") or "")
        if ctx is None:
            deferred.append(Site(src[: call.start()].count("\n") + 1, func.group("name")))
            continue
        name = ctx.group(1)
        if name == "_":
            name = "ctx"
            needs_ctx_name[func.start("params")] = func
        # Replace the "(" that opens the call with "Context(<ctx>, ".
        edits.append((call.end() - 1, 1, f"Context({name}, "))

    for params_start, func in needs_ctx_name.items():
        params = func.group("params")
        renamed = params.replace("_ context.Context", "ctx context.Context", 1)
        if renamed != params:
            edits.append((params_start, len(params), renamed))

    # Apply back to front so earlier offsets stay valid.
    edits.sort(key=lambda edit: -edit[0])
    for pos, old_len, new in edits:
        src = src[:pos] + new + src[pos + old_len :]

    if edits and not dry_run:
        path.write_text(src)
    return len(edits), deferred


def targets(args: list[str]) -> list[pathlib.Path]:
    out: list[pathlib.Path] = []
    for arg in args:
        p = pathlib.Path(arg)
        if p.is_dir():
            out.extend(sorted(f for f in p.rglob("*.go") if not f.name.endswith("_test.go")))
        elif p.suffix == ".go" and not p.name.endswith("_test.go"):
            out.append(p)
    return out


def main() -> int:
    args = [a for a in sys.argv[1:] if a != "--dry-run"]
    dry_run = "--dry-run" in sys.argv[1:]
    if not args:
        print(__doc__, file=sys.stderr)
        return 2

    total_edits = 0
    total_deferred: list[tuple[pathlib.Path, Site]] = []
    for path in targets(args):
        edits, deferred = migrate(path, dry_run)
        if edits:
            total_edits += edits
            print(f"{path}: {edits} rewritten")
        total_deferred.extend((path, site) for site in deferred)

    if total_deferred:
        print(f"\n{len(total_deferred)} call sites need a judgement (no context in scope):")
        for path, site in total_deferred:
            print(f"  {path}:{site.line} in {site.func}")

    print(f"\nrewritten {total_edits}, deferred {len(total_deferred)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
