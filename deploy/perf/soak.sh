#!/bin/bash
# The production-shape soak as one runnable item (docs/performance.md §9):
# cmd/loadgen drives the fleet, cmd/soakcheck samples the plateau series,
# deploy/perf/drills.sh injects the two §7 deployment-level faults mid-run,
# and the process exits with soakcheck's verdict — 0 only when every series
# holds a plateau over a window the checker actually observed.
#
#   ./deploy/perf/soak.sh --scale 1k --duration 24h
#   ./deploy/perf/soak.sh --plan --scale 5k --duration 24h      # review first
#   ./deploy/perf/soak.sh --scale 100 --duration 120s --interval 10s \
#                        --warmup 4 --drills none               # lab smoke
#
# The script never manages the stack lifecycle: it requires the HA stack
# already up and healthy (atlas1/atlas2 reporting healthy, every --metrics URL
# answering) and leaves it running when it finishes. Per-replica admin ports
# must be published for the verdict to be per replica — the shipped :8082 is
# HAProxy-fronted and sticky, so one URL cannot produce two series. Publish
# them with a small ports override and pass the URLs via --metrics.
#
# An interrupt does not throw the run away: soakcheck appends each tick to the
# TSV as it happens, so on SIGINT/SIGTERM the verdict is still taken from
# whatever window was recorded.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
COMPOSE_FILE="$HERE/../docker-compose.ha.yaml"
DRILLS="$HERE/drills.sh"

SCALE="1k"
DURATION="24h"
INTERVAL="60s"
DRIFT="0.20"
WARMUP="" # empty = the scale preset's default
DRILLS_MODE="both"
REPLICA_DOWN="30s"
PG_DOWN="20s"
METRICS="http://127.0.0.1:18082/metrics http://127.0.0.1:28082/metrics"
PROJECT="${PROJECT:-atlas-ha}"
PUBLIC="http://127.0.0.1:8080"
REGISTRY="http://127.0.0.1:8081"
ADMIN="http://127.0.0.1:8082"
OUT=""
LOADGEN=""
SOAKCHECK=""
SEED="1"
PLAN=false
SMOKE=""

usage() {
  cat <<'EOF'
usage: soak.sh [--scale 100|1k|5k] [--duration 24h] [--interval 60s] [--drift 0.20]
               [--warmup N] [--drills both|replica-kill|pg-outage|none]
               [--down 30s] [--pg-down 20s] [--metrics URL [URL ...]]
               [--project atlas-ha] [--public URL] [--registry URL] [--admin URL]
               [--file PATH] [--out DIR] [--loadgen PATH] [--soakcheck PATH]
               [--seed 1] [--plan]

The runner-only --override-smoke accepts exactly "servers=N read-rps=N" and is
not a general escape hatch; run.sh uses it only for the 100 preset with a 75%
smoke drift tolerance and three warm-up ticks.

  --scale     size preset; sets the §9 shape (read/char rps, ops cadence, read
              workers, soakcheck warm-up default):
                100 → 100 read rps, 1 char rps,  ops 10s, 16 read workers, warm-up 2
                1k  → 200 read rps, 20 char rps, ops 5s,  16 read workers, warm-up 4
                5k  → 500 read rps, 50 char rps, ops 5s,  32 read workers, warm-up 6
  --duration  soak length; ≥24h is the §9 acceptance, shorter runs are lab evidence
  --interval  soakcheck scrape period
  --drift     tolerated leading-vs-trailing quarter drift (0.20 = 20%)
  --warmup    samples dropped per series before judging, so the boot herd stays
              out of the plateau window (default: the preset's)
  --drills    mid-run faults folded in per §9: replica-kill at 1/3 (atlas1) and
              2/3 (atlas2) of the window, pg-outage at 1/2
  --down      replica-kill outage length   --pg-down: pg-outage outage length
  --metrics   one URL per replica's admin /metrics (repeat the flag or pass a
              space-separated list; the sticky HAProxy front cannot stand in)
  --public / --registry / --admin   loadgen's base URLs (shipped-compose defaults)
  --out       archive dir (default soak-<scale>-<isotimestamp> in the cwd)
  --loadgen / --soakcheck   existing binaries; default: built from this repo
  --plan      print the schedule and the exact commands, then exit
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --scale) SCALE="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    --interval) INTERVAL="$2"; shift 2 ;;
    --drift) DRIFT="$2"; shift 2 ;;
    --warmup) WARMUP="$2"; shift 2 ;;
    --drills) DRILLS_MODE="$2"; shift 2 ;;
    --down) REPLICA_DOWN="$2"; shift 2 ;;
    --pg-down) PG_DOWN="$2"; shift 2 ;;
    --metrics) shift; METRICS=""; while [ $# -gt 0 ] && [ "${1#-}" = "$1" ]; do METRICS="$METRICS $1"; shift; done ;;
    --project) PROJECT="$2"; shift 2 ;;
    --public) PUBLIC="$2"; shift 2 ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    --admin) ADMIN="$2"; shift 2 ;;
    --file) COMPOSE_FILE="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --loadgen) LOADGEN="$2"; shift 2 ;;
    --soakcheck) SOAKCHECK="$2"; shift 2 ;;
    --seed) SEED="$2"; shift 2 ;;
    --plan) PLAN=true; shift ;;
    --override-smoke) SMOKE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

log() { echo "$(date -Is) $*"; }

# Seconds from "90s" / "30m" / "24h" / plain seconds; anything else is rejected
# rather than guessed, so a typo cannot silently shorten a 24h soak.
to_seconds() {
  case "$1" in
    [0-9]*h) echo $(( ${1%h} * 3600 )) ;;
    [0-9]*m) echo $(( ${1%m} * 60 )) ;;
    [0-9]*s) echo "${1%s}" ;;
    [0-9]*) echo "$1" ;;
    *) echo "invalid duration (use 90s / 30m / 24h or plain seconds): $1" >&2
       exit 2 ;;
  esac
}

# The §9 scale points, pinned to the shapes the local runs used.
case "$SCALE" in
  100) SERVERS=100;  READ_RPS=100; CHAR_RPS=1;  OPS=10s; WORKERS=16; PRESET_WARMUP=2 ;;
  1k)  SERVERS=1000; READ_RPS=200; CHAR_RPS=20; OPS=5s;  WORKERS=16; PRESET_WARMUP=4 ;;
  5k)  SERVERS=5000; READ_RPS=500; CHAR_RPS=50; OPS=5s;  WORKERS=32; PRESET_WARMUP=6 ;;
  *) echo "unknown --scale $SCALE (use 100, 1k or 5k)" >&2; exit 2 ;;
esac
[ -n "$WARMUP" ] || WARMUP="$PRESET_WARMUP"

if [ -n "$SMOKE" ]; then
  if [ "$SCALE" != "100" ] || [ "$DRILLS_MODE" != "none" ] || [ "$WARMUP" != "3" ] || [ "$DRIFT" != "0.75" ] \
    || [[ ! "$SMOKE" =~ ^servers\=[1-9][0-9]*\ read-rps\=[1-9][0-9]*$ ]]; then
    echo "--override-smoke is reserved for run.sh's 100-preset workstation gate" >&2
    exit 2
  fi
  SERVERS="${SMOKE#servers=}"
  SERVERS="${SERVERS%% *}"
  READ_RPS="${SMOKE#*read-rps=}"
  WORKERS=3
fi

case "$DRILLS_MODE" in
  both|replica-kill|pg-outage|none) ;;
  *) echo "unknown --drills $DRILLS_MODE (both|replica-kill|pg-outage|none)" >&2; exit 2 ;;
esac

DUR_S=$(to_seconds "$DURATION")
IV_S=$(to_seconds "$INTERVAL")
if [ "$DUR_S" -lt 60 ]; then
  echo "a soak shorter than 60s cannot reach the 8-sample minimum; raise --duration" >&2
  exit 2
fi
KILL1=$(( DUR_S / 3 ))
PG=$(( DUR_S / 2 ))
KILL2=$(( 2 * DUR_S / 3 ))

if [ -z "$OUT" ]; then
  OUT="soak-${SCALE}-$(date +%Y%m%dT%H%M%S)"
fi

plan() {
  cat <<EOF
soak plan
  scale        $SCALE ($SERVERS servers, $READ_RPS read rps, $CHAR_RPS char rps, ops $OPS, $WORKERS read workers)
  window       $DURATION (${DUR_S}s), soakcheck every $INTERVAL (${IV_S}s), drift $DRIFT, warm-up $WARMUP
  metrics      $METRICS
  endpoints    public $PUBLIC / registry $REGISTRY / admin $ADMIN
  drills       $DRILLS_MODE (replica-kill --down $REPLICA_DOWN, pg-outage --pg-down $PG_DOWN)
  archive      $OUT
drill schedule (offsets into the window)
  t=${KILL1}s  replica-kill atlas1
  t=${PG}s  pg-outage
  t=${KILL2}s  replica-kill atlas2
loadgen
  $LOADGEN -servers $SERVERS -duration $DURATION -read-rps $READ_RPS -read-workers $WORKERS -char-rps $CHAR_RPS -ops-every $OPS -admin $ADMIN -registry $REGISTRY -public $PUBLIC -seed $SEED -report $OUT/loadgen.json
soakcheck
  $SOAKCHECK --interval $INTERVAL --duration $DURATION --drift $DRIFT --warmup $WARMUP --out $OUT/soak.tsv --metrics $METRICS
verdict
  $SOAKCHECK --sample $OUT/soak.tsv --interval $INTERVAL --warmup $WARMUP
EOF
}

if $PLAN; then
  LOADGEN="${LOADGEN:-loadgen}"
  SOAKCHECK="${SOAKCHECK:-soakcheck}"
  plan
  exit 0
fi

case "$OUT" in /*) ;; *) OUT="$PWD/$OUT" ;; esac
mkdir -p "$OUT"

if [ -z "$LOADGEN" ]; then
  log "building cmd/loadgen"
  ( cd "$REPO" && go build -o "$OUT/loadgen" ./cmd/loadgen ) > "$OUT/build.log" 2>&1
  LOADGEN="$OUT/loadgen"
fi
if [ -z "$SOAKCHECK" ]; then
  log "building cmd/soakcheck"
  ( cd "$REPO" && go build -o "$OUT/soakcheck" ./cmd/soakcheck ) >> "$OUT/build.log" 2>&1
  SOAKCHECK="$OUT/soakcheck"
fi

# Preflight: no soak without a healthy stack and reachable replica metrics.
[ -n "$METRICS" ] || { log "ERROR no --metrics URLs; the verdict needs one URL per replica" >&2; exit 2; }
log "preflight: stack $PROJECT must be healthy, metrics must answer"
for svc in atlas1 atlas2; do
  cid="$(docker compose -f "$COMPOSE_FILE" -p "$PROJECT" ps -q "$svc" 2>/dev/null || true)"
  if [ -z "$cid" ]; then
    log "ERROR $svc is not running in project $PROJECT; bring the stack up first" >&2
    exit 2
  fi
  status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$cid" 2>/dev/null || true)"
  if [ "$status" != "healthy" ]; then
    log "ERROR $svc reports $status; bring the stack up first" >&2
    exit 2
  fi
done
for u in $METRICS; do
  if ! curl -fsS --max-time 5 "$u" > /dev/null 2>&1; then
    log "ERROR $u not answering — publish per-replica admin ports and pass them via --metrics" >&2
    exit 2
  fi
done

plan > "$OUT/plan.txt"

run_drill() { # offset then the drills.sh drill arguments
  local off="$1"; shift
  ( sleep "$off"
    log "drill window opening at t=${off}s"
    "$DRILLS" "$@" --project "$PROJECT" --file "$COMPOSE_FILE" >> "$OUT/drills.log" 2>&1
    log "drill window closed at t=${off}s" ) &
}

# On interrupt: stop the drivers, then judge whatever landed on disk — the TSV
# is written per tick, so nothing before the interrupt is lost.
on_interrupt() {
  log "interrupted; stopping drivers and taking the verdict from the recorded window"
  kill "${GEN_PID:-}" "${CHECK_PID:-}" 2>/dev/null || true
  "$SOAKCHECK" --sample "$OUT/soak.tsv" --interval "$INTERVAL" --warmup "$WARMUP" > "$OUT/verdict.txt" 2>&1 || true
  cat "$OUT/verdict.txt"
  exit 130
}
trap on_interrupt INT TERM

log "starting soakcheck: every $INTERVAL, drift $DRIFT, warm-up $WARMUP, record $OUT/soak.tsv"
# --metrics takes the whole list as one quoted word: a bare second URL would be
# a stray positional and soakcheck refuses those rather than misjudge the run.
"$SOAKCHECK" --interval "$INTERVAL" --duration "$DURATION" \
  --drift "$DRIFT" --warmup "$WARMUP" --out "$OUT/soak.tsv" \
  --metrics "$METRICS" > "$OUT/soakcheck.log" 2>&1 &
CHECK_PID=$!

log "starting loadgen: $SERVERS servers, $READ_RPS read rps / $CHAR_RPS char rps / ops $OPS / $WORKERS read workers, seed $SEED"
"$LOADGEN" -servers "$SERVERS" -duration "$DURATION" -read-rps "$READ_RPS" -read-workers "$WORKERS" \
  -char-rps "$CHAR_RPS" -ops-every "$OPS" -admin "$ADMIN" -registry "$REGISTRY" \
  -public "$PUBLIC" -seed "$SEED" -report "$OUT/loadgen.json" > "$OUT/loadgen.log" 2>&1 &
GEN_PID=$!

case "$DRILLS_MODE" in
  both|replica-kill)
    run_drill "$KILL1" replica-kill --service atlas1 --down "$REPLICA_DOWN"
    run_drill "$KILL2" replica-kill --service atlas2 --down "$REPLICA_DOWN"
    ;;
esac
case "$DRILLS_MODE" in
  both|pg-outage)
    run_drill "$PG" pg-outage --down "$PG_DOWN"
    ;;
esac

wait "$GEN_PID" || log "WARNING loadgen exited non-zero (see $OUT/loadgen.log)"
wait "$CHECK_PID" || log "WARNING soakcheck exited non-zero (see $OUT/soakcheck.log)"
wait || log "WARNING a drill exited non-zero (see $OUT/drills.log)"

log "verdict from the recorded window (replayable: $SOAKCHECK --sample $OUT/soak.tsv --interval $INTERVAL --warmup $WARMUP)"
set +e
"$SOAKCHECK" --sample "$OUT/soak.tsv" --interval "$INTERVAL" --warmup "$WARMUP" 2>&1 | tee "$OUT/verdict.txt"
rc=${PIPESTATUS[0]}
set -e
log "soak complete; verdict exit=$rc; archive in $OUT"
exit "$rc"
