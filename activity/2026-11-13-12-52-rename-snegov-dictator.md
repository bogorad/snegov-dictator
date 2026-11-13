# Rename snegov-dictator

Renamed the local repository from `snegov-dictatr` to `snegov-dictator` at the operator's request. Renamed the Dolt database from `snegov_dictatr` to `snegov_dictator` on `doltsrv.lan:3307`. This follows the `bd-init` rule that maps repository hyphens to database underscores.

Updated `.beads/metadata.json` and the Beads issue prefix to use the corrected name. Preserved the project identifier, Git history, database schema, all 76 original Dolt commits, and the database working state. Removed the old database after verification.

A full recovery backup remains at `file:///tmp/snegov-dictator-rename-8f8d476b-20261113` on the Dolt server. This is temporary server storage. The rename did not require a server restart or Git publication.
