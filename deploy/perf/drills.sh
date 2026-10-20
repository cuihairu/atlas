#!/bin/bash
# Mid-run fault drills for the production-shape soak (docs/performance.md §9),
# against the representative topology in ../docker-compose.ha.yaml.
#
# §7 leaves two scenarios at deployment level instead of the unit suite —
# "Atlas replica goes down" and "PG briefly unavailable". Both get folded
# into the 24h soak mid-run, so this script is what the operator runs next
# to cmd/loadgen instead of hand-typing docker commands at an arbitrary hour:
#
#   ./deploy/perf/drills.sh replica-kill --down 30s
#   ./deploy/perf/drills.sh pg-outage    --down 20s
#
# Each drill prints an ISO timestamp at perturbation start and end so a
# loadgen report can be correlated against the injected window. The script
# exits 0 only if the service comes back healthy; a drill that leaves the
# stack degraded fails loudly so it shows up in the soak log.
#
# Drills perturb the deployment, not Atlas's correctness: during the window
# some flows are expected to error (writes through a dead PG, reads through
# a stopped replica). The pass bar is recovery without a manual Atlas
# restart — pgxpool and HAProxy must recycle the bad leg on their own.
set -euo pipefail

COMPOSE_FILE="$(cd "$(dirname "$0")" && pwd)/../docker-compose.ha.yaml"
PROJECT="${PROJECT:-atlas-ha}"
SVC=""
DOWN=30s
LIST=false

usage() {
  cat <<'EOF'
usage: drills.sh {replica-kill|pg-outage|list} [--project NAME] [--down 30s]
                       [--service atlas2] [--file PATH]

  replica-kill   stop one Atlas replica, wait --down, bring it back
  pg-outage      stop the PostgreSQL primary, wait --down, bring it back
  list           print the services of the compose project and exit

  --project  atlas-ha      compose project name (env PROJECT)
  --down     30s           outage length (s or plain seconds)
  --service  atlas2        replica to hit (atlas1 / atlas2, for coverage
                           of both replicas across a long soak)
  --file     PATH          compose file (default: this repo's HA stack)
EOF
}

drill=""
while [ $# -gt 0 ]; do
  case "$1" in
    replica-kill|pg-outage) drill="$1"; shift ;;
    list) LIST=true; shift ;;
    --project) PROJECT="$2"; shift 2 ;;
    --service) SVC="$2"; shift 2 ;;
    --down) DOWN="$2"; shift 2 ;;
    --file) COMPOSE_FILE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

log() { echo "$(date -Is) $*"; }

# Seconds in "30s" or "30"; anything else (1m, 10ms) is rejected rather than
# guessed, so a typo cannot silently shorten an outage.
to_seconds() {
  case "$1" in
    [0-9][0-9]*s|[0-9][0-9]*) echo "${1%s}" ;;
    *) echo "invalid duration (use 30s or plain seconds): $1" >&2; exit 2 ;;
  esac
}

# Wait until the container behind service $1 reports healthy, or give up at
# $2 seconds. Health comes from docker inspect (compose's own ps filtering
# differs across versions); services without a healthcheck would report an
# empty status forever and time out loudly.
wait_healthy() {
  local svc="$1" to start cid status
  to=$(to_seconds "$2")
  start=$(date +%s)
  while [ $(( $(date +%s) - start )) -lt "$to" ]; do
    cid="$(docker compose -f "$COMPOSE_FILE" -p "$PROJECT" ps -q "$svc" 2>/dev/null || true)"
    if [ -n "$cid" ]; then
      status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$cid" 2>/dev/null || true)"
      if [ "$status" = "healthy" ]; then
        log "$svc healthy (after $(( $(date +%s) - start ))s)"
        return 0
      fi
    fi
    sleep 2
  done
  log "ERROR $svc did not report healthy within ${to}s" >&2
  return 1
}

if $LIST; then
  docker compose -f "$COMPOSE_FILE" -p "$PROJECT" ps
  exit 0
fi

if [ -z "$drill" ]; then
  usage >&2
  exit 2
fi

case "$drill" in
  replica-kill)
    [ -n "$SVC" ] || SVC="atlas2"
    ;;
  pg-outage)
    SVC="postgres-primary"
    ;;
esac

log "drill=$drill service=$SVC project=$PROJECT down=$DOWN"
docker compose -f "$COMPOSE_FILE" -p "$PROJECT" stop "$SVC"
log "$SVC stopped; outage window open"
sleep "$(to_seconds "$DOWN")"
docker compose -f "$COMPOSE_FILE" -p "$PROJECT" start "$SVC"
log "$SVC started; waiting for healthy"
wait_healthy "$SVC" 180s
log "drill=$drill complete; stack back to healthy"
