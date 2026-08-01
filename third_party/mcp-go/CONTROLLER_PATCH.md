# Controller patch

This is `github.com/mark3labs/mcp-go` v0.57.0, pinned through the root
`go.mod`. The controller fork changes only the JSON-RPC error seam:

- the client retains upstream `code`, `message`, and `data` in a typed error;
- the server recognizes that typed error and writes the same JSON-RPC error.

Without this patch, direct forwarding turns every upstream protocol error into
an internal `-32603` error and discards its data payload.
