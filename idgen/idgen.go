package idgen

import (
	aetheridgen "github.com/BabySid/aether/idgen"
	"github.com/google/uuid"
)

type Generator struct{}

var _ aetheridgen.Generator = Generator{}

func (Generator) Generate(_ aetheridgen.Context) string {
	return uuid.NewString()
}
