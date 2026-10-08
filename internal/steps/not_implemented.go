package steps

import (
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// notImplemented is the error of a step that is decoded from the configuration but has no
// implementation yet. The milestone says when it arrives (spec section 1). When a step gets its
// implementation, its Run stops calling this.
func notImplemented(step workflow.Step, milestone string) error {
	return diag.Newf("Step %s is not implemented yet (milestone %s)", step.Type(), milestone).
		WithCode("remeris::step_not_implemented")
}
