# Dictator static reading companion

This directory is a self-contained static website. It contains the introduction,
all 73 chapter summaries, 14 reference pages, and the public book evidence.
Every page works with normal links and without JavaScript. HTMX adds partial
page navigation when a web server is available.

## Build

From the repository root, with Go and Pandoc installed:

    go run scripts/build-kepub.go
    go run scripts/build-site.go
    go run scripts/build-site.go -check

The builder reads DICTATOR_SUMMARY.md, docs/knowledge/*.md, the six part ledgers,
docs/knowledge/source-map.json, web/template.html, and web/assets/. It copies
the public Russian EPUB source from src/, the named paragraph caches from
tmp/chapter-text/, and source-map JSON and TSV evidence. It never copies the
repository metadata or operational records. The canonical Markdown and source
files remain unchanged. A reader-facing reference landing replaces the catalog
maintenance instructions only in this generated website.

The build checks the exact chapter order, source hashes, local link targets,
fragment anchors, and generated duplicate IDs. The manifest tracks only files
owned by the builder. Modified or conflicting output files cause a failure;
unrelated files are preserved. Output is restricted to the repository's site/
directory. Do not edit generated files; update their source and rebuild.

## Preview

From the repository root:

    go run scripts/serve-site.go

Open http://127.0.0.1:4173/. Use -root site or -addr 127.0.0.1:4174 to select
another folder or local port. Press Ctrl-C to stop the preview server.

## Static hosting

Use the sorted paths in tmp/site/public-files.txt (relative to this directory)
to copy only public files to a static host. The list excludes this README and
the internal build manifest. It includes every reader page, each linked asset
or evidence file, and the HTMX distribution license. No runtime backend,
package manager, JavaScript build tool, or external asset service is required.
Relative URLs support hosting at either a domain root or a directory prefix.
The overview has EPUB and Kobo KEPUB downloads near the end of the page. Their
canonical files are in web/ and are copied into this directory by the builder.
The required Kobo download extension for this project is .kepub. The filename
is snegov-dictator-kepub.kepub; kepubify uses --calibre to retain that extension.
Do not add a single-page-app rewrite rule: each HTML address is a real file.
Opening index.html directly also supports normal reading and navigation;
HTMX enhancement needs HTTP. This build does not publish or deploy the site.

The local HTMX license is in assets/HTMX-LICENSE.txt. Its version record remains
in the repository at web/assets/HTMX-VERSION.txt. Original book source files are bundled unchanged in
source/; paragraph caches and machine-readable source evidence are in evidence/.
