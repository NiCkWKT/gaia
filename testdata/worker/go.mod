module gaia/testdata/worker

go 1.26.5

require (
	github.com/BabySid/aether v0.0.0
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/BabySid/aether => github.com/NiCkWKT/aether v0.0.0-20260928030500-a23faa85122a
