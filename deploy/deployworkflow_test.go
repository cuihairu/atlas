package deploy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// deploy-docker.yml carries a hand-rolled bash gate that decides whether the
// live demo host gets touched. Its failure modes are quiet: one wrong variable
// and the gate polls a commit that will never gain runs (run 45676037203
// stalled for its whole budget on a docs-only HEAD), a pull split wrong and a
// poisoned registry fails the deploy (run 45685014935). None of that surfaces
// in CI, because the workflow only executes on the self-hosted deploy runner.
// These tests pin the load-bearing semantics to the workflow text the same way
// the perf tests pin soak.sh: cheap anchors against silent regressions.

func workflowPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", ".github", "workflows", "deploy-docker.yml")
}

func workflowText(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(workflowPath(t))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	return string(raw)
}

func bash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
}

// stepBlocks splits the workflow into step chunks keyed by their `- name:`
// lines at the job's step indentation level (six spaces in this file).
func stepBlocks(t *testing.T, text string) map[string]string {
	t.Helper()
	blocks := make(map[string]string)
	var name string
	var body []string
	flush := func() {
		if name != "" {
			if _, dup := blocks[name]; dup {
				t.Fatalf("duplicate step name %q", name)
			}
			blocks[name] = strings.Join(body, "\n")
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "      - name: ") {
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(line, "      - name: "))
			body = nil
			continue
		}
		if name != "" {
			body = append(body, line)
		}
	}
	flush()
	if len(blocks) == 0 {
		t.Fatal("no step blocks found; step indentation changed?")
	}
	return blocks
}

// runBody returns the block-scalar body of a `run: |` inside a step chunk,
// dedented the way YAML block-scalar semantics (and therefore the Actions
// runner) present it to bash — heredoc terminators sit at column 0 there.
func runBody(t *testing.T, chunk string) string {
	t.Helper()
	lines := strings.Split(chunk, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " ") == "        run: |" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("no `run: |` block in step chunk:\n%s", chunk)
	}
	const indent = "          "
	var out []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, indent) {
			break
		}
		out = append(out, strings.TrimPrefix(line, indent))
	}
	return strings.Join(out, "\n")
}

func want(t *testing.T, what, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: missing %q", what, needle)
	}
}

func wantNot(t *testing.T, what, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%s: must not contain %q", what, needle)
	}
}

func TestWorkflowRunBlocksParse(t *testing.T) {
	bash(t)
	for name, chunk := range stepBlocks(t, workflowText(t)) {
		if !strings.Contains(chunk, "run: |") {
			continue
		}
		script := filepath.Join(t.TempDir(), "step.sh")
		body := runBody(t, chunk)
		if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Errorf("step %q: bash -n failed: %v\n%s", name, err, out)
		}
	}
}

// TestGatePollsTheDeployedTagCommit pins the 2026-11-26 gate fix (f3aac42):
// the gate must wait for runs on the commit the image tag names, never on the
// dispatch-time HEAD (a docs-only HEAD has no runs at all and stalls the whole
// budget), and a commit without run records must walk to the nearest
// run-bearing ancestor instead of polling forever.
func TestGatePollsTheDeployedTagCommit(t *testing.T) {
	text := workflowText(t)
	gate := runBody(t, stepBlocks(t, text)["Gate — main green (ci + image)"])

	want(t, "env", text, "TAG: ${{ inputs.tag }}")
	wantNot(t, "env", text, "SHA: ${{ github.sha }}")

	want(t, "gate", gate, `runs?head_sha=$sha&per_page=1`)
	wantNot(t, "gate", gate, `head_sha=$SHA`)

	// Resolution: "latest" means the newest successful build's commit; an
	// explicit tag resolves through /commits/.
	want(t, "gate", gate, `if [ "$TAG" = "latest" ]`)
	want(t, "gate", gate, `docker-image.yml/runs?status=success&per_page=1`)
	want(t, "gate", gate, `repos/$REPO/commits/$TAG`)

	// Lineage check, with status and behind_by extracted as separate fields:
	// they are not adjacent in the JSON ("ahead_by" sits in between), so a
	// single contiguous pattern never matches an ancestor commit.
	want(t, "gate", gate, `compare/$GATE_SHA...main`)
	want(t, "gate", gate, `"status":"[^"]*"' | head -1 | cut -d'"' -f4`)
	want(t, "gate", gate, `"behind_by":[0-9]*`)
	want(t, "gate", gate, `[ "$cmp_status" != "identical" ]`)
	want(t, "gate", gate, `[ "$cmp_status" != "ahead" ] || [ "$cmp_behind" != "0" ]`)
	want(t, "gate", gate, `is not on the main lineage`)
	want(t, "gate", gate, `refusing to deploy`)

	// Ancestor walk: a commit with zero run records was skipped by
	// paths-ignore, so its CI-relevant content equals the nearest ancestor
	// that ran. Walk one parent up; fail loud if there is no parent.
	want(t, "gate", gate, `"total_count":[0-9]*`)
	want(t, "gate", gate, `grep -o '"parents":\[{"sha":"[^"]*"'`)
	want(t, "gate", gate, `gating on ancestor`)
	want(t, "gate", gate, `no run for $sha and no parent commit; cannot gate`)

	// The gate's commit travels to the summary for the deployment record.
	want(t, "gate", gate, `GATE_SHA=$GATE_SHA" >> "$GITHUB_ENV`)
}

// TestPullRequiresGHCRAndFallsBackForDockerIO pins the pull split (9901511):
// GHCR images are the payload and must resolve remotely, while docker.io base
// images are best-effort — that registry resolves to a poisoned IP more
// often than not from the deploy host, and the images are already cached
// locally by an earlier deploy.
func TestPullRequiresGHCRAndFallsBackForDockerIO(t *testing.T) {
	text := workflowText(t)
	pull := runBody(t, stepBlocks(t, text)["Pull images"])

	ghcr := strings.Index(pull, `pull atlas dashboard demoagents`)
	base := strings.Index(pull, `pull migrate postgres redis`)
	if ghcr < 0 || base < 0 {
		t.Fatalf("pull steps missing (ghcr at %d, base at %d)", ghcr, base)
	}
	if ghcr > base {
		t.Error("GHCR payload pull must run before the best-effort base-image pull")
	}
	want(t, "pull", pull, `docker image inspect postgres:17-alpine redis:8-alpine`)

	// The fallback names must match the compose file: a base image bump
	// there has to update the cached-copy check here.
	composeRaw, err := os.ReadFile(filepath.Join(filepath.Dir(workflowPath(t)), "..", "..", "deployments", "docker", "docker-compose.deploy.yml"))
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	compose := string(composeRaw)
	want(t, "compose", compose, `image: postgres:17-alpine`)
	want(t, "compose", compose, `image: redis:8-alpine`)
}

// TestRollbackOnlyRunsAfterPrepare pins the rollback guard: a failure before
// "Prepare deploy directory" never touched the host, so the rollback step
// must not run (it used to fire on any earlier failure, report the
// first-deploy message and add a failure of its own).
func TestRollbackOnlyRunsAfterPrepare(t *testing.T) {
	text := workflowText(t)
	rollback := stepBlocks(t, text)["Rollback to previous images"]
	want(t, "rollback", rollback,
		`if: failure() && (env.PREV_ATLAS_TAG != '' || env.PREV_DASHBOARD_TAG != '')`)
}

// TestCheckoutBudgetSurvivesStalledFetch pins the checkout budget: the deploy
// host's route to github.com stalls mid-TLS, and the fetch's internal retry
// needs more than the earlier 3-minute budget to ride out a bad window.
func TestCheckoutBudgetSurvivesStalledFetch(t *testing.T) {
	chunk := stepBlocks(t, workflowText(t))["Checkout"]
	m := regexp.MustCompile(`timeout-minutes: (\d+)`).FindStringSubmatch(chunk)
	if m == nil {
		t.Fatal("checkout step has no timeout-minutes")
	}
	minutes, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if minutes < 8 {
		t.Errorf("checkout timeout %dm is below the 8m a stalled fetch retry needs", minutes)
	}
}
