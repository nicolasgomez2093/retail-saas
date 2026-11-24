#!/bin/sh
# Daily logical backup: one compressed pg_dump per day, older files removed after KEEP_DAYS.
set -eu
umask 077
while :; do
  stamp=$(date -u +%Y%m%d-%H%M%S)
  cd /backups
  if pg_dump --format=custom --file="retail-$stamp.dump.partial"; then
    mv "retail-$stamp.dump.partial" "retail-$stamp.dump"
    # Relative name so the checksum also verifies on the host: cd /var/backups/retail && sha256sum -c <file>.sha256
    sha256sum "retail-$stamp.dump" > "retail-$stamp.dump.sha256"
    echo "backup ok retail-$stamp.dump"
  else
    rm -f "retail-$stamp.dump.partial"
    echo "backup FAILED $stamp" >&2
  fi
  find /backups -name 'retail-*.dump*' -mtime +"$KEEP_DAYS" -delete
  sleep 86400
done
