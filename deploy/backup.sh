#!/bin/sh
# Consistent backup of the database and media. Run from cron, e.g. daily:
#   15 3 * * * /opt/amiga/deploy/backup.sh /var/backups/amiga
set -eu
DEST="${1:?usage: backup.sh <dest-dir>}/$(date +%Y%m%d-%H%M%S)"
export AMIGA_DATA_DIR="${AMIGA_DATA_DIR:-/var/lib/amiga}"
/opt/amiga/amiga backup "$DEST"
# Keep the last 14 backups.
ls -1dt "${1%/}"/*/ 2>/dev/null | tail -n +15 | xargs -r rm -rf
echo "backup written to $DEST"
