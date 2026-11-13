# EPUB and Kobo KEPUB downloads

Created `web/snegov-dictator.epub` and `web/snegov-dictator-kepub.kepub.epub`, containing the English introduction, all 73 chapter summaries, and the internal reference catalog. The standard file is 374,325 bytes; the Kobo file is 454,802 bytes. The Kobo edition is an actual kepubify 4.0.4 conversion, with 14,620 Kobo spans and the required layout wrappers.

Public format research used Exa. Sources: https://github.com/kobolabs/epub-spec, https://pgaskin.net/kepubify/docs/, and https://pkg.go.dev/github.com/pgaskin/kepubify/v4/kepub. The `.kepub.epub` suffix selects Kobo's renderer; the internal conversion supplies reading-position markers. Reflowable styling leaves device fonts, size and colors available. The original Russian novel is not bundled; English provenance records explain source locators.

Both archives passed package/XML/UTF-8/navigation/link checks. Independent checks confirmed ordered chapter IDs, Kobo structures and preserved visible text. The website passed 46 Chromium checks, including actual downloads that matched both source files, mobile placement and HTMX bypass. The two links are in the Read offline section below the overview text. Formal EPUBCheck and physical Kobo testing were unavailable; no such claim is made.

The 240 serving files were copied to the authorized remote public directory. All remote hashes match. Both public HTTPS downloads return application/epub+zip and byte-match the local files; the public homepage also matches the generated HTML. Preview processes are stopped. Canonical prose and the user's Git index remain unchanged; no commit or push was made.

EPUB SHA-256: `f72e81d16eff93d82553cdf236ea61a07d9bce7d2febac677edf5dfd9ea8fdbe`.
KEPUB SHA-256: `e509ab231049a4893c3551e4e5ebe6ed51ee32439e5dc6b9e8b628b2843488e9`.
