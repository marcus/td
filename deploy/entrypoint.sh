#!/bin/sh
set -e

# Restore from replica if exists (first boot on new host)
litestream restore -if-replica-exists -config /etc/litestream.yml /data/server.db || true

# Run td-sync wrapped by litestream replication
exec litestream replicate -exec "td-sync" -config /etc/litestream.yml
