#!/usr/bin/env sh
set -e

if [ -n "$PUID" ] && [ "$PUID" -ne 0 ] && \
   [ -n "$PGID" ] && [ "$PGID" -ne 0 ] ; then
  echo "info: creating the new user vikunja with $PUID:$PGID"
  groupmod -g "$PGID" vikunja
  usermod -u "$PUID" vikunja
  chown -R vikunja:vikunja ./
  exec su vikunja -c /app/vikunja/vikunja "$@"
else
  echo "info: creation of non-root user is skipped"
  exec /app/vikunja/vikunja "$@"
fi