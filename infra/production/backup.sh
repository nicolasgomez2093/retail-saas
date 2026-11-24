#!/bin/sh
# Daily logical backup: one compressed pg_dump per day, older files removed after KEEP_DAYS.
set -eu
umask 077
while :; do
  stamp=$(date -u +%Y%m%d-%H%M%S)
  if pg_dump --format=custom --file="/backups/retail-$stamp.dump.partial"; then
    mv "/backups/retail-$stamp.dump.partial" "/backups/retail-$stamp.dump"
    sha256sum "/backups/retail-$stamp.dump" > "/backups/retail-$stamp.dump.sha256"
    echo "backup ok retail-$stamp.dump"
  else
    rm -f "/backups/retail-$stamp.dump.partial"
    echo "backup FAILED $stamp" >&2
  fi
  find /backups -name 'retail-*.dump*' -mtime +"$KEEP_DAYS" -delete
  sleep 86400
done
