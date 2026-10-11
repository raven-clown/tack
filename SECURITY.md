# Security Policy

## Supported versions

| Version | Supported |
| --- | --- |
| Latest minor release | ✅ |
| Older releases | ❌ — please upgrade |

Before v1.0, only the latest minor version receives security fixes. After v1.0, the latest two minor versions will be supported.

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

Instead, use one of these private channels:

1. **GitHub private vulnerability reporting** (preferred): go to the [Security tab](https://github.com/raven-clown/tack/security) and click **Report a vulnerability**.
2. **Email:** security@your-domain.example (replace with a real address).

Please include:

- A description of the issue and its impact.
- Steps to reproduce, ideally a minimal `main.go`.
- The tack version and Go version affected.
- Any suggested fix, if you have one.

## What to expect

| Step | Target time |
| --- | --- |
| Acknowledge your report | within 3 days |
| Initial assessment | within 7 days |
| Fix released (for confirmed issues) | as soon as possible, typically within 30 days |

We will keep you informed throughout, and credit you in the release notes unless you prefer to remain anonymous.

## Scope

In scope: request binding and parsing, validation bypasses, information leaks in error responses, denial of service through unbounded input, and generated code (OpenAPI, TypeScript) that is unsafe.

Out of scope: vulnerabilities in your own handlers or middleware, and issues in third-party dependencies (please report those upstream — we'll update promptly).
