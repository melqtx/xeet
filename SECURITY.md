# Security Policy

Xeet imports and stores X session cookies (`auth_token`, `ct0`), which grant
account-level access. Vulnerabilities in how those credentials are read,
stored, transmitted, or printed are the highest-priority reports.

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Please do not open a public issue for security problems.

Report privately via
[GitHub private vulnerability reporting](https://github.com/melqtx/xeet/security/advisories/new).
You should receive an initial response within a week.

When reporting, include the xeet version (`xeet version`), your OS, and steps
to reproduce. Never include real cookie values, HAR files, or `xeet doctor`
output from an account you care about — redact first.

## Scope notes

- Cookies are stored in the macOS Keychain or Linux Secret Service, never in
  the YAML config file. Anything that causes them to be written to disk,
  logs, or terminal output is a vulnerability.
- Xeet talks only to X-operated hosts (`x.com`, `upload.twitter.com`,
  `*.twimg.com`, `t.co`). Any request to another host — especially one
  carrying cookies — would be a vulnerability.
- Attacks requiring an already-compromised local account (e.g. reading
  another process's keychain with the user's own privileges) are generally
  out of scope, but reports are still welcome.
