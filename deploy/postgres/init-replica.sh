#!/bin/bash
# Runs once on the PostgreSQL primary (docker-entrypoint-initdb.d):
# allow the replica to stream WAL as the atlas user. See docs/ha.md §4.
set -e

echo "host replication atlas 0.0.0.0/0 scram-sha-256" >> "$PGDATA/pg_hba.conf"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -c "ALTER ROLE atlas WITH REPLICATION;"
