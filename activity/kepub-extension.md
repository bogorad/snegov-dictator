# Kobo download extension

The operator set `.kepub` as the required Kobo download extension for this
project and renamed the file to `web/snegov-dictator-kepub.kepub`.

Updated the builder, website download URL, browser checks, and documented
convention. The existing converted book bytes are preserved. Future conversion
uses kepubify's `--calibre` option to retain `.kepub`.
