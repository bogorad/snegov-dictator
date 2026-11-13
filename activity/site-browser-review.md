# Website browser review

Date: 2026-11-13.

Scope: the local static reading site. The product brief requires HTMX navigation,
neutral light colors, Catppuccin Mocha dark colors, and a storm SVG favicon.
The global HTMX decision requires full-page navigation without scripts.

The review adds `scripts/verify-site.go`. It uses Go's standard library and a real
headless Chromium process through the DevTools pipe. Each run uses a temporary,
isolated browser profile, then removes it and stops Chromium. No browser package
was installed. The user's normal browser profile was not used. No Jev or
Hindsight was used.

Reproduction, from the repository root in Bash:

```bash
go run scripts/serve-site.go
```

In a second Bash session:

```bash
go run scripts/verify-site.go
```

The preview serves `site/` at `http://127.0.0.1:4173`. The check writes its results,
browser events, browser log, and screenshots to `tmp/site-test/`. That directory
is local test evidence and is not part of the publication set.

The first browser pass found two defects. Browser history restores sent HTMX
swap events without `detail.target`, which caused script errors. The persistent
relative favicon URL also resolved against later chapter and catalog paths,
which caused HTTP 404 responses. Both findings were sent to the interface owner.

Checks cover full pages, all 73 expected chapter files, actual HTMX requests,
URL and title changes without document reload, previous and next links, browser
history, nested catalog routes, source-map hash placement, sidebar links, theme
colors and persistence, chapter filtering and clear, keyboard skip navigation,
visible focus, narrow layouts, mobile contents, reduced motion, source and cached
text links, and navigation with page scripts disabled. Text colors are checked
against a 4.5 contrast ratio. Screenshots are reviewed for line length, spacing,
legibility, and horizontal overflow.

Final status: PASS. The repaired build passed all 40 browser and serving checks.
The browser recorded eight real HTMX requests. There were no script exceptions
and no HTTP errors for site assets after enhanced navigation. The two defects
are fixed and the final checks exercise both repairs.

The final visual review covers `desktop-light.png`, `desktop-dark.png`,
`desktop-chapter-light.png`, `mobile-light.png`, `mobile-dark.png`,
`mobile-contents.png`, `mobile-320-catalog.png`, and `mobile-320-table.png` in
`tmp/site-test/`. Desktop uses a 1440 by 1000 viewport. Mobile uses Chromium's
mobile viewport emulation at 390 by 844 and 320 by 640. Long catalog tables scroll
inside their own area. They do not widen the page. The light and dark reading
views are legible and use the requested colors.

The original source page probes for `/favicon.ico`, which returns 404. That
legacy page has no reading-site shell. Its text and styles load, and its link
opens as a full document. The site's requested SVG favicon returns HTTP 200
with the correct image type, including after enhanced navigation.

The preview process on port 4173 and all owned Chromium processes are stopped.
The temporary browser profiles are removed. No product files were edited by
this reviewer. No commits, pushes, or publication were done. Root owns remote
publication and the final task status.

Limits: Chromium was tested. Safari, Firefox, a physical touch device, and a
screen reader were not tested. Source text views use the original book styles.
The review checks site behavior and sample rendered content; it does not repeat
the book's narrative review.
