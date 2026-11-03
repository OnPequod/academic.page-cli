# Project API contract update

Projects now use an ordered links list and a published boolean. The embedded
OpenAPI spec matches Pequod's project refactor. Publishing flags and MCP tools
respect either a boolean or timestamp publishing field, as described by the
operation's schema. Other content types retain their existing date behavior.

Project links are supplied through --data, matching the CLI's existing handling
of lists of objects. Retired project URL and tools flags are unavailable.

Validation: go test ./..., go vet ./..., and a local binary build passed. A new
regression covers draft creation, publishing, unpublishing, titled links, and
rejection of the retired URL flag, without network writes.

This commit is prepared locally for the matching app release. No version tag,
release, or push was performed.
