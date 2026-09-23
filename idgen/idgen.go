package idgen

import (
	aetheridgen "github.com/BabySid/aether/idgen"
	"github.com/google/uuid"
)

// Generator generates UUIDv4 identifiers for workflow and task runs.
type Generator struct{}

var _ aetheridgen.Generator = Generator{}

func (Generator) Generate(_ aetheridgen.Context) string {
	return uuid.NewString()
}
