package memory

import (
	"testing"

	"github.com/cuihairu/atlas/internal/store/storetest"
)

// The memory store runs the shared behavioral contract on every `go test` —
// it is the reference implementation the SQL stores are held against.
func TestContract(t *testing.T) {
	s := New()
	storetest.Run(t, s, s)
	storetest.RunRuntime(t, s)
}
