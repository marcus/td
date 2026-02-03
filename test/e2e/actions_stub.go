package e2e

import "math/rand"

// ActionDef defines a chaos action with a name and executor.
// This is a stub — the full action registry will be built in a later task.
type ActionDef struct {
	Name   string
	Weight int
	Exec   func(e *ChaosEngine, actor string) ActionResult
}

// SelectAction picks a weighted random action from the registry.
// Stub: returns a no-op action until the action registry is implemented.
func SelectAction(rng *rand.Rand) ActionDef {
	return ActionDef{
		Name:   "noop",
		Weight: 1,
		Exec: func(e *ChaosEngine, actor string) ActionResult {
			return ActionResult{OK: true}
		},
	}
}
