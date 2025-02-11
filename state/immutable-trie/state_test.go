package itrie

import (
	"testing"

	"github.com/ankr/dogesyncer/state"
)

func TestState(t *testing.T) {
	state.TestState(t, buildPreState)
}

func buildPreState(pre state.PreStates) (state.State, state.Snapshot) {
	storage := NewMemoryStorage()
	st := NewState(storage, nil)
	snap := st.NewSnapshot()

	return st, snap
}
