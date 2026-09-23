module gaia

go 1.26.5

require (
	github.com/BabySid/aether v0.0.0
	github.com/go-chi/chi/v5 v5.3.2
)

require (
	github.com/google/uuid v1.6.0
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

// The NiCkWKT fork still declares its module path as github.com/BabySid/aether.
replace github.com/BabySid/aether => github.com/NiCkWKT/aether v0.0.0-20260923033312-efc94c1fa82a
