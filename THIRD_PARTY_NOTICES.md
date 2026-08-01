# Third-party notices

MCP Workbench Proxy is derived from `smart-mcp-proxy/mcpproxy-go` and is
distributed under the MIT license included in [LICENSE](LICENSE).

The repository contains a patched source copy of `mark3labs/mcp-go` under
`third_party/mcp-go`. That project is distributed under the MIT license; its
license text is included alongside the vendored source.

Go module dependencies and their versions are recorded in `go.mod` and
`go.sum`. Release CI generates a dependency inventory and SBOM for every
published binary.
