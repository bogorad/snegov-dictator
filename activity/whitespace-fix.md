# Deterministic whitespace repair

Removed extra blank lines at EOF from 16 authored files. The summary assembler now emits exactly one final newline. A second normalization pass changed no files.

The imported EPUB, exact website source copies, and tab-separated evidence remain byte-identical. Narrow `.gitattributes` rules preserve their original line endings and allow the original XML spacing and meaningful empty final fields. Whitespace checks remain active for authored Markdown and code.

Source-hash checks, all 73 chapter identities, 1,261 document links/anchors, and the static-site check pass. The repair changes formatting and Git data-file classification only; no narrative text or ebook payload was changed.
