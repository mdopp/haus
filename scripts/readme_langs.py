#!/usr/bin/env python3
"""The README in four languages: README.md (English) and README.de.md,
README.es.md, README.fr.md next to it (AGENTS.md "README in four languages").

CI fails when

- a translation is missing, or has another number of headings than the
  English one (lines starting with '#', outside code blocks): a section was
  added or dropped in one language only;
- given a base ref (`scripts/readme_langs.py origin/main`, as a pull request
  runs it): README.md changed since that base and a translation did not.
  A change to a translation alone is fine (a better word).

Python's standard library and git, nothing else.
"""
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
ENGLISH = "README.md"
TRANSLATIONS = ["README.de.md", "README.es.md", "README.fr.md"]


def headings(text):
    count, fenced = 0, False
    for line in text.splitlines():
        if line.lstrip().startswith("```"):
            fenced = not fenced
        elif not fenced and line.startswith("#"):
            count += 1
    return count


def changed_since(base):
    out = subprocess.run(
        ["git", "diff", "--name-only", f"{base}...HEAD"],
        cwd=ROOT, check=True, capture_output=True, text=True,
    ).stdout
    return set(out.split())


def main(argv):
    errors = []
    want = headings((ROOT / ENGLISH).read_text(encoding="utf-8"))
    for name in TRANSLATIONS:
        path = ROOT / name
        if not path.exists():
            errors.append(f"{name} is missing")
            continue
        got = headings(path.read_text(encoding="utf-8"))
        if got != want:
            errors.append(f"{name} has {got} headings, {ENGLISH} has {want}")
    if len(argv) > 1:
        changed = changed_since(argv[1])
        if ENGLISH in changed:
            for name in TRANSLATIONS:
                if name not in changed:
                    errors.append(f"{ENGLISH} changed since {argv[1]}, {name} did not")
    for e in errors:
        print(f"error: {e} (update all four READMEs in the same PR)")
    if not errors:
        print(f"README: four languages, {want} headings each")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
