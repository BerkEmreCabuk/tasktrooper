package resources

import _ "embed"

// ConfigYAML and OpenAPIYAML are compiled into the binary because the packaged
// desktop app ships one executable and no resources directory beside it. The
// files on disk are still the source of truth for editing; CONFIG_PATH points
// the server at a different one when somebody wants to.
//
//go:embed config.yml
var ConfigYAML []byte

//go:embed openapi.yaml
var OpenAPIYAML []byte
