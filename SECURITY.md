# Security Policy

## Reporting a Vulnerability

Please do not open a public issue for a suspected vulnerability.

Until this project has a dedicated security contact, report issues privately to
the current maintainer or repository owner. Include:

- Affected version or commit.
- Steps to reproduce.
- Expected impact.
- Any logs or proof of concept needed to validate the report.

The maintainers will acknowledge the report, investigate, and coordinate a fix
before public disclosure when the issue is confirmed.

## Scope

ATM runs local commands and delegates prompts to external agent CLIs selected by
the user. Treat todo files as executable workflow input, and review third-party
todo files before running them.
