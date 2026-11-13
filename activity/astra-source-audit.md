# Independent source audit

Date: 2026-11-13. Executor: GPT-6 Astra. Scope: EPUB source mapping and the
earlier narrow `bd-init` finding. No Hindsight, Jev, TypeSafe, or other model
was used for this audit.

Result: **PASS after three tooling and metadata corrections.** The inherited
chapter caches were already complete. All 73 chapter caches and the front
matter cache keep their original bytes and paragraph IDs. No reader range
changed. The six active readers were cleared through the main thread.

## Independent proof

The new [audit program](../scripts/audit-book-source.go) uses Go's XML parser.
It does not call the inherited Perl extraction functions. It reads
[content.opf](../src/content.opf), [toc.ncx](../src/toc.ncx), and every physical
HTML/XHTML file in `src/`. It derives ownership from ordered NCX part and
chapter events. Part events return ownership to front matter. Chapter events
start the next chapter. Continuation files retain the current chapter.

The audit compares the physical HTML set, manifest set, and spine set. They
are equal: 73 files, consisting of one image-only cover and 72 text files.
The XML parser found no non-whitespace body text outside paragraph elements.
It compares every paragraph, source file, anchor byte offset, global range,
owner, text hash, byte count, cache row, and complete cache file. It also
checks all JSON chapter copies, the coverage TSV, and Markdown chapter rows.

| Check | Verified result |
| --- | --- |
| Parts / chapters | 6 / 73 |
| Chapters per part | 10, 9, 13, 15, 11, 15 |
| Source paragraph blocks | 6,109 |
| Chapter-owned blocks | 6,095 |
| Front matter blocks | 14 |
| Source text, UTF-8 bytes | 2,560,418 |
| Front matter text, UTF-8 bytes | 326 |
| Empty paragraph blocks | 2 |
| Preserved nonbreaking spaces | 8,481 |
| Entity references in source text files | 0 |
| Missing or duplicated extraction blocks | 0 |

Counts include chapter headings and empty paragraphs. Text-byte totals exclude
cache IDs, file names, tabs, and row endings. ASCII layout whitespace is
normalized. Unicode spaces and source spelling are retained. The leading
nonbreaking space in `P4-C15-P0046` is real source content; it is retained.

| Boundary case | Verified source ownership |
| --- | --- |
| `part0002.html` split | P1-C01: 139 blocks; P1-C02: 36 blocks; second anchor attribute starts at byte 58,177 |
| P3-C04 continuation | `part0023.html`: 33; `part0024.html`: 135; total 168 |
| P3-C13 short chapter | 5 blocks in `part0031.html`; anchor attribute starts at byte 47,757 |
| P5-C08 continuation | `part0053.html`: 179; `part0054.html`: 16; total 195 |
| P5-C11 continuation | `part0057.html`: 116; `part0058.html`: 17; total 133 |
| P6-C15 final file | 64 blocks in `part0071.html`, including both self-closing empty paragraphs and the author's closing date/place |

`end_global` is the next chapter start. Its interval can include a later
part's title blocks, which belong to front matter. Thus P3-C13 has five owned
blocks despite an interval width of seven. Ownership, not interval width,
determines the chapter count.

## Corrections

1. The inherited JSON map double-encoded the six Russian part titles. English
   `title` labels now match the Markdown map. `source_title` holds the exact
   decoded Russian NCX text.
2. The inherited verifier checked source hashes and owner counts but did not
   compare cache contents. `scripts/book-source verify` now runs the independent
   XML audit and fails on cache text, order, owner, or metadata differences.
3. The inventory console printed interval width as paragraph count. It now
   prints the actual owned count. This corrects the P3-C13 and P5-C11 reports.

The extractor now decodes and re-encodes paragraph text explicitly as UTF-8.
It preserves Unicode spaces. Unchanged outputs are not rewritten. The source
map explains count and byte units. The root thread's earlier correction of
the front text total to 326 bytes passed the independent check.

Changed paths: `scripts/book-source`, `scripts/audit-book-source.go`,
`docs/knowledge/source-map.json`, `docs/knowledge/source-map.md`, and this note.
The coverage TSV and all cache files remain byte-identical. Source files and
the database were not changed.

## Reproduction and checks

Run from `/home/chuck/git/snegov-dictator` in Bash:

```bash
scripts/book-source verify
go run scripts/audit-book-source.go
perl -c scripts/book-source
go vet scripts/audit-book-source.go
rtk git diff --check
bd --readonly context --json
bd --readonly status --json
```

All commands passed. The first two commands are the same independent audit
through two entry points. Successful audit output has `"failures": null` and
exit status zero. The audit derives expected text from the original files on
each run; it does not trust the cached text or the ownership ledger as inputs.

A separate temporary fixture changed the first chapter's heading from `1`
to `9`. File size and paragraph count stayed equal. The audit returned nonzero
and reported `cache byte/content/order differs: P1-C01`. The fixture used a
read-only source link and copied outputs; active reader caches were untouched.
The temporary test script passed ShellCheck. The fixture is
`/tmp/astra-source-negative.L1dGuz`; its result is time-bounded test evidence.

The source control files used for this check have SHA-256 values:

```text
content.opf  7f902605a71a8ff72f94c424a8092853c2420cb3ae960ed4ce58ed1c3d2d4f48
toc.ncx      e08ef0368d91b90864019a2ac9b2684b28a67e59be7ec275f05e953d0aa1c6de
```

## Narrow bd-init finding

Independent static review of
`/persist/nix-config/nx-ago-testing/home-manager/scripts/bd-init.sh` confirms:

- Lines 157–172 replace each non-alphanumeric repository-name character with
  `_`; a leading digit receives an additional `_` prefix.
- Lines 694–695 derive the database name from the Git root's basename.
- Lines 414–430 select an existing database or conditionally create a new one.
  Existing local metadata plus a missing target database causes failure.
- The script contains no database rename or data-copy operation. Running it
  after a repository rename cannot rename the old database.

The read-only live context returned repository `snegov-dictator`, database
`snegov_dictator`, server mode, server `doltsrv.lan:3307`, no redirect, and no
worktree. The read-only status query succeeded. This confirms the active
mapping and server access at audit time. `bd-init` was not executed.

## Limits and handoff

This is a complete text-extraction audit for this supplied EPUB. It is not a
generic EPUB validator, a visual cover review, a translation review, or a
literary reading check. OPF descriptions and HTML head metadata are outside the
novel-body text universe. The chapter readers own semantic reading evidence.

No database migration, repository rename, source edit, commit, or push was
performed. No audit process remains running. The main thread owns Beads
updates and the Matrix completion report. No source-audit blocker remains.
