# Security Policy

## Project status

Seelex is a Developer Alpha and is not presented as a hardened sandbox or an unattended production service. The current file and command boundary is project-scope path gating plus permission policy; it is not OS-level process, network or container isolation.

Only the latest code on the default branch is actively evaluated for security fixes. Historical tags may not receive backports.

## Reporting a vulnerability

Please use GitHub's private **Report a vulnerability** / Security Advisory flow for this repository. Do not open a public Issue for an unpatched vulnerability and do not include real credentials, account files, session data or private endpoints in a report.

Include, when possible:

- affected commit or tag;
- attack preconditions and impact;
- a minimal reproduction using synthetic data;
- whether the issue crosses ProjectScope, permission, provider, plugin, MCP, persistence or release-package boundaries;
- a proposed mitigation, if known.

There is currently no guaranteed response-time SLA. The maintainer will acknowledge, reproduce and coordinate disclosure as capacity permits. Please allow a reasonable remediation window before public disclosure.

## High-value review areas

Reports are especially useful for path traversal, command policy bypass, secret leakage, archive contamination, cross-project/session data access, unsafe plugin or MCP activation, approval bypass, provider credential exposure and persistence corruption.
