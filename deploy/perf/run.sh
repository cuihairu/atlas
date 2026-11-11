#!/bin/bash
# The §9 acceptance runner: one command brings up the shipped HA topology,
# runs the production-shaped load model, samples replica telemetry, captures
# per-endpoint p95/p99 reports, and archives every artifact under one
# timestamped directory.
#
#   make perf-runner-smoke
#   deploy/perf/run.sh --plan --scale 1k --duration 24h
#   deploy/perf/run.sh --scale 1k --duration 24h
#
# Only --scale smoke is intended for a developer workstation. The 100/1k/5k
# presets remain §9 acceptance runs and must be launched on a designated
# machine with the capacity for the requested window.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
COMPOSE_FILE="$HERE/../docker-compose.ha.yaml"
SOAK="$HERE/soak.sh"
PROJECT="${ATLAS_PERF_PROJECT:-atlas-perf-runner}"
SCALE="smoke"
RUN_SCALE=""
DURATION=""
DRILLS="none"
SEED="1"
OUT=""
KEEP="false"
PLAN="false"
METRICS="http://127.0.0.1:18082/metrics http://127.0.0.1:28082/metrics"
PUBLIC="http://127.0.0.1:38080"
REGISTRY="http://127.0.0.1:38081"
ADMIN="http://127.0.0.1:38882"
COMPOSE_ENV=()

usage() {
  cat <<'EOF'
usage: run.sh [--scale smoke|100|1k|5k] [--duration 45s|24h] [--drills both|replica-kill|pg-outage|none]
              [--seed 1] [--project atlas-perf-runner] [--file PATH] [--out DIR] [--keep] [--plan]

  --scale smoke is the workstation gate: 3 nodes for 60s, no drills.
                100/1k/5k preserve the §9 soak presets and are not smoke tests.
  --duration    smoke defaults to 60s; acceptance scales follow the §9 defaults.
  --out         archive directory (default: perf-<scale>-<timestamp>)
  --keep        leave the HA stack running after an acceptance run (never applies
                to smoke; smoke always tears its stack down)
  --plan        print the plan without changing Docker or starting a run

Artifacts: plan.txt, soak.log, loadgen.json, loadgen.log, soak.tsv, verdict.txt,
endpoints.json, endpoints.txt, summary.json, and summary.txt.
Smoke drops the first three 5s scrapes per series: a 60s Go process has a real
runtime warm-up phase, and it must not be misread as a 24h soak leak.
Smoke's plateau gate covers goroutines and fds; heap is archived without gating
because a 60s Go process can legitimately cross generations.
Runner-side ports: public 38080, registry 38081, admin 38882, per-replica 18082/28082.
Set ATLAS_PERF_PORTS=shared to use the shipped 8080/8081/8082 defaults.
EOF
}

log() { echo "$(date -Is) $*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --scale) SCALE="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    --drills) DRILLS="$2"; shift 2 ;;
    --seed) SEED="$2"; shift 2 ;;
    --project) PROJECT="$2"; shift 2 ;;
    --file) COMPOSE_FILE="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --keep) KEEP=true; shift ;;
    --plan) PLAN=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$SCALE" in
  smoke)
    RUN_SCALE="100"
    DURATION="${DURATION:-60s}"
    ;;
  100|1k|5k)
    RUN_SCALE="$SCALE"
    [ -n "$DURATION" ] || { echo "--duration is required for --scale $SCALE" >&2; exit 2; }
    ;;
  *) echo "unknown --scale $SCALE (use smoke, 100, 1k or 5k)" >&2; exit 2 ;;
esac

for tool in docker curl go python3; do
  command -v "$tool" >/dev/null 2>&1 || { echo "missing required tool: $tool" >&2; exit 2; }
done
docker compose version >/dev/null 2>&1 || { echo "docker compose v2 is required" >&2; exit 2; }
[ -f "$COMPOSE_FILE" ] || { echo "compose file not found: $COMPOSE_FILE" >&2; exit 2; }

if [ -z "$OUT" ]; then
  OUT="$REPO/deploy/perf/results/perf-${SCALE}-$(date +%Y%m%dT%H%M%S)"
fi
if [ "${ATLAS_PERF_PORTS:-runner}" != shared ]; then
  COMPOSE_ENV=(env ATLAS_HA_PUBLIC_PORT=38080 ATLAS_HA_REGISTRY_PORT=38081 ATLAS_HA_ADMIN_PORT=38882 \
    ATLAS_HA_ATLAS1_ADMIN_PORT=18082 ATLAS_HA_ATLAS2_ADMIN_PORT=28082)
  PUBLIC=http://127.0.0.1:38080
  REGISTRY=http://127.0.0.1:38081
  ADMIN=http://127.0.0.1:38882
fi
RUN_WARMUP=3
RUN_DRIFT=0.75
if [ "$SCALE" != smoke ]; then
  RUN_WARMUP=0
  RUN_DRIFT=0.20
fi
case "$OUT" in /*) ;; *) OUT="$PWD/$OUT" ;; esac
mkdir -p "$OUT"
PLAN_FILE="$OUT/plan.txt"

{
  echo "perf runner plan"
  echo "  scale        $SCALE"
  echo "  soak preset  $RUN_SCALE"
  echo "  topology     $COMPOSE_FILE"
  echo "  project      $PROJECT"
  echo "  duration     $DURATION"
  echo "  drills       $DRILLS"
  echo "  seed         $SEED"
  echo "  archive      $OUT"
  echo "  metrics      $METRICS"
  echo "  endpoints    public $PUBLIC / registry $REGISTRY / admin $ADMIN"
  echo "  reports      loadgen.json endpoints.json endpoints.txt soak.tsv verdict.txt summary.json summary.txt"
  echo "  soak command $SOAK --scale ${RUN_SCALE:-unset} --duration $DURATION --interval 5s --drift $RUN_DRIFT --warmup $RUN_WARMUP --drills $DRILLS --metrics $METRICS --project $PROJECT --file $COMPOSE_FILE --out $OUT --seed $SEED"
  if [ "$SCALE" = smoke ]; then
    echo "smoke override"
    echo "  $SOAK substitutes loadgen -servers 3 -read-rps 5 while preserving the 60s telemetry window"
    echo "  smoke gate     loadgen zero-error plus both replicas' goroutine/fd plateau; heap is recorded but not gated"
  fi
} > "$PLAN_FILE"
cat "$PLAN_FILE"

compose() {
  env "${COMPOSE_ENV[@]}" docker compose -f "$COMPOSE_FILE" -p "$PROJECT" "$@"
}

# PIPESTATUS is only valid on the statement directly after the pipeline, so the
# capture sits immediately after the tee: read past a function return — or after
# even a bare `[` test — it yields 0, and the smoke gate would exit 0 on FAIL.
run_soakcheck_verdict() {
  set +e
  "$OUT/soakcheck" --sample "$OUT/soak.tsv" --interval 5s --drift "$RUN_DRIFT" \
    --warmup "$RUN_WARMUP" 2>&1 | tee "$OUT/verdict.txt"
  smoke_verdict_rc=${PIPESTATUS[0]}
  set -e
}

smoke_verdict_rc=0

write_summary() {
  python3 - "$OUT" <<'PY'
import datetime as dt
import json
import pathlib
import sys

out = pathlib.Path(sys.argv[1])

def load(name):
    try:
        return json.loads((out / name).read_text())
    except Exception:
        return {}

loadgen = load("loadgen.json")
verdict = next((line for line in (out / "verdict.txt").read_text(errors="replace").splitlines()
                if line.startswith("verdict:")), "verdict: MISSING")
endpoint_text = (out / "endpoints.txt").read_text(errors="replace")
verdict_ok = verdict.startswith("verdict: PASS")
endpoint_ok = endpoint_text.strip().splitlines()[0] == "class               count errors p95_ms p99_ms flows"
plateau_gate = True
for line in (out / "verdict.txt").read_text(errors="replace").splitlines():
    if "go_memstats_heap_inuse_bytes" in line or "warm-up" in line or line.startswith("soakcheck plateau"):
        continue
    if line.startswith("http://") and not line.endswith("plateau"):
        plateau_gate = False
report = {
    "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
    "scale": out.name,
    "verdict": verdict,
    "endpoint_classes": {},
    "error_budget": {"total_errors": 0, "total_skipped": 0},
    "ok": False,
}
for stream in loadgen.get("streams", []):
    report["endpoint_classes"][stream["name"]] = {
        "count": stream["count"],
        "errors": stream["errors"],
        "skipped": stream["skipped"],
        "p50_ms": stream["p50_ms"],
        "p95_ms": stream["p95_ms"],
        "p99_ms": stream["p99_ms"],
        "rps": stream["rps"],
    }
    report["error_budget"]["total_errors"] += stream["errors"]
    report["error_budget"]["total_skipped"] += stream["skipped"]
report["ok"] = (
    verdict_ok
    and endpoint_ok
    and plateau_gate
    and report["error_budget"]["total_errors"] == 0
    and report["error_budget"]["total_skipped"] == 0
)
(out / "summary.json").write_text(json.dumps(report, indent=2) + "\n")
lines = [f"perf runner summary — {out.name}", verdict]
for name in sorted(report["endpoint_classes"]):
    s = report["endpoint_classes"][name]
    lines.append(f"{name:18} count={s['count']:8} p95={s['p95_ms']:5}ms p99={s['p99_ms']:5}ms errors={s['errors']} skipped={s['skipped']}")
lines.append(f"errors={report['error_budget']['total_errors']} skipped={report['error_budget']['total_skipped']}")
(out / "summary.txt").write_text("\n".join(lines) + "\n")
print("\n".join(lines))
PY
}

wait_healthy() {
  local deadline=$((SECONDS + 180))
  while [ "$SECONDS" -lt "$deadline" ]; do
    local statuses
    statuses="$(compose ps --format json atlas1 atlas2 2>/dev/null || true)"
    if [ "$(grep -c '"Health":"healthy"' <<<"$statuses" 2>/dev/null || true)" -ge 2 ]; then
      return 0
    fi
    sleep 2
  done
  compose ps
  compose logs --no-color --tail=80 atlas1 atlas2 haproxy postgres-primary redis-sentinel-1 2>&1 | tail -120 > "$OUT/bootstrap-failure.log" || true
  return 1
}

teardown() {
  local rc=$?
  if [ "$SCALE" = smoke ] || [ "$KEEP" != true ]; then
    log "tearing down $PROJECT"
    compose down -v --remove-orphans >> "$OUT/compose.log" 2>&1 || true
  else
    log "leaving $PROJECT running as requested"
  fi
  exit "$rc"
}

if $PLAN; then
  "$SOAK" --plan --scale "$RUN_SCALE" --duration "$DURATION" --interval 5s --drift "$RUN_DRIFT" \
    --warmup "$RUN_WARMUP" \
    --drills "$DRILLS" --metrics $METRICS --project "$PROJECT" --file "$COMPOSE_FILE" \
    --out "$OUT" --seed "$SEED" --public "$PUBLIC" --registry "$REGISTRY" --admin "$ADMIN" >> "$PLAN_FILE"
  cat "$PLAN_FILE"
  exit 0
fi

trap teardown EXIT INT TERM
log "building HA stack $PROJECT"
compose up -d --build >> "$OUT/compose.log" 2>&1
if ! wait_healthy; then
  log "ERROR Atlas replicas did not become healthy; see $OUT/bootstrap-failure.log"
  exit 1
fi
log "running acceptance pipeline"
SOAK_ARGS=(--scale "$RUN_SCALE" --duration "$DURATION" --interval 5s --drift "$RUN_DRIFT"
  --warmup "$RUN_WARMUP" --drills "$DRILLS" --metrics $METRICS --project "$PROJECT" --file "$COMPOSE_FILE"
  --out "$OUT" --seed "$SEED" --public "$PUBLIC" --registry "$REGISTRY" --admin "$ADMIN")
if [ "$SCALE" = smoke ]; then
  SOAK_ARGS+=(--override-smoke "servers=3 read-rps=5")
fi
set +e
"$SOAK" "${SOAK_ARGS[@]}" 2>&1 | tee "$OUT/soak.log"
soak_rc=${PIPESTATUS[0]}
set -e

run_soakcheck_verdict

set +e
python3 - "$OUT" <<'PY'
import json
import pathlib
import sys

out = pathlib.Path(sys.argv[1])
if not (out / "loadgen.json").exists():
    print("perf runner summary — pipeline failed before loadgen finished")
    raise SystemExit(0)
raw = json.loads((out / "loadgen.json").read_text())
classes = {
    "register": "register",
    "heartbeat": "heartbeat",
    "read-list": "read",
    "read-detail": "read",
    "read-recommend": "read",
    "char-write": "character-write",
    "ops-stats": "admin",
}
grouped = {}
for stream in raw.get("streams", []):
    cls = classes.get(stream["name"])
    if not cls:
        continue
    grouped.setdefault(cls, []).append(stream)
rows = []
for cls in ("register", "heartbeat", "read", "character-write", "admin"):
    members = grouped.get(cls, [])
    if not members:
        continue
    rows.append({
        "class": cls,
        "flows": sorted(s["name"] for s in members),
        "count": sum(s["count"] for s in members),
        "errors": sum(s["errors"] for s in members),
        "p95_ms": max(s["p95_ms"] for s in members),
        "p99_ms": max(s["p99_ms"] for s in members),
    })
(out / "endpoints.json").write_text(json.dumps({"classes": rows}, indent=2) + "\n")
lines = ["class               count errors p95_ms p99_ms flows"]
for row in rows:
    lines.append(f"{row['class']:18} {row['count']:6} {row['errors']:5} {row['p95_ms']:6} {row['p99_ms']:6} {','.join(row['flows'])}")
(out / "endpoints.txt").write_text("\n".join(lines) + "\n")
PY
report_rc=$?
set -e
if [ "$report_rc" -ne 0 ]; then
  exit "$report_rc"
fi
write_summary
if [ "$SCALE" = smoke ]; then
  exit "$smoke_verdict_rc"
fi
exit "$soak_rc"
