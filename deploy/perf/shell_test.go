package perf_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The §9 soak tooling is two bash scripts next to this test. They cannot be
// unit-tested meaningfully without a docker stack, but their cheap failure
// modes can: a syntax error, a bad flag, and a plan that silently miscomputes
// the drill schedule would all surface only on the acceptance machine.

func script(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, name)
}

func bash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
}

func TestSoakAndDrillsParse(t *testing.T) {
	bash(t)
	for _, name := range []string{"soak.sh", "drills.sh"} {
		out, err := exec.Command("bash", "-n", script(t, name)).CombinedOutput()
		if err != nil {
			t.Errorf("%s: bash -n failed: %v\n%s", name, err, out)
		}
	}
}

func TestSoakPlanComputesTheDrillSchedule(t *testing.T) {
	bash(t)
	out, err := exec.Command("bash", script(t, "soak.sh"),
		"--plan", "--scale", "5k", "--duration", "24h").CombinedOutput()
	if err != nil {
		t.Fatalf("soak.sh --plan: %v\n%s", err, out)
	}
	plan := string(out)
	// 24h folds the drills in at thirds and a half; a plan that drifts from
	// that schedule is a plan the operator cannot trust.
	for _, needle := range []string{
		"t=28800s  replica-kill atlas1",
		"t=43200s  pg-outage",
		"t=57600s  replica-kill atlas2",
		"500 read rps, 50 char rps, ops 5s, 32 read workers",
		"warm-up 6",
		"--sample",
	} {
		if !strings.Contains(plan, needle) {
			t.Errorf("plan missing %q:\n%s", needle, plan)
		}
	}
	// --metrics takes a list, and the flag package stops parsing at the first
	// bare URL — a soakcheck line with flags after it runs on defaults and
	// ignores --duration. It must end the command.
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "soakcheck") && strings.Contains(line, "--metrics") {
			if i, j := strings.LastIndex(line, "--metrics"), strings.LastIndex(line, "--out"); i < 0 || j < 0 || i < j {
				t.Errorf("soakcheck line does not end with --metrics:\n%s", line)
			}
		}
	}
}

func TestSoakPlanCarriesScalePresets(t *testing.T) {
	bash(t)
	cases := []struct{ scale, shape, warmup string }{
		{"100", "100 read rps, 1 char rps, ops 10s, 16 read workers", "warm-up 2"},
		{"1k", "200 read rps, 20 char rps, ops 5s, 16 read workers", "warm-up 4"},
		{"5k", "500 read rps, 50 char rps, ops 5s, 32 read workers", "warm-up 6"},
	}
	for _, c := range cases {
		out, err := exec.Command("bash", script(t, "soak.sh"),
			"--plan", "--scale", c.scale).CombinedOutput()
		if err != nil {
			t.Errorf("scale %s: %v\n%s", c.scale, err, out)
			continue
		}
		plan := string(out)
		for _, needle := range []string{c.shape, c.warmup} {
			if !strings.Contains(plan, needle) {
				t.Errorf("scale %s plan missing %q:\n%s", c.scale, needle, out)
			}
		}
	}
}

func TestSoakRejectsBadArguments(t *testing.T) {
	bash(t)
	bad := [][]string{
		{"--scale", "9k"},
		{"--drills", "sometimes"},
		{"--duration", "soon"},
		{"--duration", "30s"}, // cannot reach the 8-sample minimum
		{"--interval", "never"},
	}
	for _, args := range bad {
		argv := append([]string{script(t, "soak.sh"), "--plan"}, args...)
		if out, err := exec.Command("bash", argv...).CombinedOutput(); err == nil {
			t.Errorf("soak.sh %v accepted (want exit 2):\n%s", args, out)
		}
	}
}

func TestRunnerPlanUsesTheShippedTopology(t *testing.T) {
	bash(t)
	// Runner tests must pass --out into a temp dir: with the default --out
	// every plan leaves a directory in deploy/perf/results/, which is the
	// §9 evidence archive, not a test scratch space.
	out, err := exec.Command("bash", script(t, "run.sh"), "--plan",
		"--out", t.TempDir()).CombinedOutput()
	if err != nil {
		t.Fatalf("run.sh --plan: %v\n%s", err, out)
	}
	plan := string(out)
	for _, needle := range []string{
		"docker-compose.ha.yaml",
		"scale        smoke",
		"soak preset  100",
		"public http://127.0.0.1:38080 / registry http://127.0.0.1:38081 / admin http://127.0.0.1:38882",
		"-servers 3",
		"-read-rps 5",
		"--duration 60s",
		"loadgen.json",
		"endpoints.json",
		"summary.json",
	} {
		if !strings.Contains(plan, needle) {
			t.Errorf("runner plan missing %q:\n%s", needle, plan)
		}
	}
}

func TestRunnerProtectsAcceptanceScales(t *testing.T) {
	bash(t)
	for _, scale := range []string{"100", "1k", "5k"} {
		out, err := exec.Command("bash", script(t, "run.sh"),
			"--plan", "--scale", scale, "--duration", "60s",
			"--out", t.TempDir()).CombinedOutput()
		if err != nil {
			t.Errorf("acceptance scale %s plan failed: %v\n%s", scale, err, out)
		}
		// The scale must reach soak.sh: with RUN_SCALE unset the run aborts
		// under set -u, and with a wrong value the fleet size silently lies.
		if !strings.Contains(string(out), "soak preset  "+scale) {
			t.Errorf("scale %s plan does not carry the preset:\n%s", scale, out)
		}
	}
	if _, err := exec.Command("bash", script(t, "run.sh"), "--plan", "--scale", "1k",
		"--out", t.TempDir()).CombinedOutput(); err == nil {
		t.Error("acceptance scale accepted without --duration (want exit 2)")
	}
	if _, err := exec.Command("bash", script(t, "run.sh"), "--plan", "--scale", "9k",
		"--out", t.TempDir()).CombinedOutput(); err == nil {
		t.Error("unknown scale accepted (want exit 2)")
	}
}

func TestRunnerVerdictContract(t *testing.T) {
	files := map[string]string{
		"endpoints.txt": "class               count errors p95_ms p99_ms flows\nregister                1     0     10     20 register\n",
		"verdict.txt":   "soakcheck plateau verdict\nhttp://atlas1 go_goroutines 2/2 1 1 +0.0% plateau\nhttp://atlas1 go_memstats_heap_inuse_bytes 2/2 1 2 +100.0% monotonic growth above tolerance\nverdict: PASS — every series holds a plateau\n",
	}
	verdict := files["verdict.txt"]
	if strings.Count(verdict, "go_memstats_heap_inuse_bytes") == 0 {
		t.Fatal("contract fixture must include the ungated heap series")
	}
	if !strings.Contains(verdict, "verdict: PASS") {
		t.Fatal("contract fixture must exercise the runner-level PASS rewrite")
	}
}

func TestRunnerCapturesTheVerdictRcOnThePipelineLine(t *testing.T) {
	bash(t)
	src, err := os.ReadFile(script(t, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "smoke_verdict_rc=${PIPESTATUS[0]}") {
			continue
		}
		// PIPESTATUS survives only until the next statement runs. The capture
		// must sit directly after the soakcheck|tee pipeline: moved past the
		// function return (or any command, even a `[` test) it reads 0 and the
		// smoke gate exits 0 on every FAIL verdict.
		if i == 0 || !strings.Contains(lines[i-1], `tee "$OUT/verdict.txt"`) {
			t.Errorf("verdict rc capture is not on the statement after the pipeline (line %d):\n%s", i+1, line)
		}
		return
	}
	t.Error("run.sh never captures the soakcheck exit code")
}
