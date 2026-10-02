package version

import "testing"

func TestString(t *testing.T) {
	oldV, oldC, oldD := Version, GitCommit, BuildDate
	t.Cleanup(func() { Version, GitCommit, BuildDate = oldV, oldC, oldD })

	Version, GitCommit, BuildDate = "v9.9.9", "abc1234", "2026-10-03T00:00:00Z"
	if got, want := String(), "v9.9.9 (abc1234, 2026-10-03T00:00:00Z)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
