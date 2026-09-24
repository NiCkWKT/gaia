module gaia

go 1.26.5

require (
	github.com/BabySid/aether v0.0.0
	github.com/go-chi/chi/v5 v5.3.2
)

require (
	github.com/expr-lang/expr v1.17.8
	github.com/go-sql-driver/mysql v1.9.3
	github.com/google/uuid v1.6.0
	github.com/stretchr/testify v1.12.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

// The NiCkWKT fork still declares its module path as github.com/BabySid/aether.
replace github.com/BabySid/aether => github.com/NiCkWKT/aether v0.0.0-20260923033312-efc94c1fa82a
