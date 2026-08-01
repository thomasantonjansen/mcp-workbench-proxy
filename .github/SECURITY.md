# Security policy

## Reporting

Please report suspected vulnerabilities privately through GitHub Security Advisories
for this repository. Do not include credentials, private MCP payloads or customer data
in a public issue.

## Supported version

Only the most recent MCP Workbench proxy release is supported with security fixes.

## Local trust model

The product build binds its MCP and management endpoints to `127.0.0.1`. Management
and controller endpoints require a locally generated API key. Agent endpoints are
unauthenticated on loopback so locally installed MCP clients can connect.

The direct forwarding path deliberately preserves upstream schemas, inputs, outputs
and protocol errors. It does not sanitize or reinterpret tool content. Users must trust
the MCP servers they install. Optional JSONL call logging may contain complete inputs,
outputs and secrets and should be protected as sensitive local data.

Please include the proxy version, operating system and a minimal reproduction in a
private report. Remove tokens, credentials and real call payloads first.
