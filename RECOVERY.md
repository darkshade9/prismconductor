# PrismConductor DB Recovery Guide

## Pre-migration backups

Before applying any pending schema migrations, PrismConductor automatically
copies `conductor.db` to a timestamped backup file in the same directory:

```
conductor.db.pre-20250504T123456Z.bak
conductor.db.pre-20250503T091000Z.bak
conductor.db.pre-20250502T080000Z.bak
```

By default the three most recent backups are retained; older ones are removed.

## When you need to restore

**Scenario A: Migration failed mid-apply**

The migration runner wraps each migration in a transaction, so a partial
migration leaves the DB unchanged. Restore is still available if something
went wrong before the migration started:

```sh
# Quit PrismConductor first, then:
cp conductor.db conductor.db.broken
cp conductor.db.pre-<most-recent-timestamp>.bak conductor.db
```

Restart PrismConductor. If the migration error persists, file an issue at
https://github.com/darkshade9/prismconductor/issues.

**Scenario B: Downgrade — newer binary wrote the DB**

If you see this error at startup:

```
conductor.db schema version "20251201_01_..." is newer than this binary (max known: "20250504_00_...")
```

…it means `conductor.db` was last opened by a newer version of PrismConductor
than the one you are running now. Options:

1. **Upgrade**: install the latest PrismConductor release.
2. **Restore from backup**: copy the most recent `.bak` file written by the
   older binary (i.e., the last backup whose timestamp predates the upgrade)
   over `conductor.db`.

```sh
cp conductor.db.pre-<timestamp-before-upgrade>.bak conductor.db
```

3. **Fresh start**: rename or delete `conductor.db`. You will lose local board
   state but the GitHub issues will re-sync on next startup.

## Finding your data directory

The data directory containing `conductor.db` is shown at startup in the logs
(`store: open …`). On macOS it is typically:

```
~/Library/Application Support/PrismConductor/
```

On Linux:

```
~/.local/share/PrismConductor/
```

## Backup location and rotation

Backups live in the same directory as `conductor.db`. The default retention is
3 backups. To keep more, set the `PRISMCONDUCTOR_BACKUP_KEEP` environment
variable before launching (not yet wired; tracked in a future issue).
