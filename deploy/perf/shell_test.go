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
