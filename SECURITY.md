# Security Policy

## Supported code

TutorHub V2 is under active development. Security fixes are applied to the latest revision of the `main` branch. No released version is currently covered by long-term support.

## Reporting a vulnerability

Do not disclose a suspected vulnerability in a public issue, discussion, pull request, screenshot or chat log. Use the repository's [private vulnerability reporting form](https://github.com/basangnguyen/TUTORHUB_WEB/security/advisories/new).

Include only the information required to reproduce and assess the issue:

- affected commit, endpoint or component;
- prerequisites and minimum reproduction steps;
- expected and observed behavior;
- security impact and affected data or roles;
- logs or screenshots with credentials, personal data and tenant data removed.

The maintainer will acknowledge a usable report as soon as practical, normally within three working days. Remediation status and a coordinated disclosure date will be agreed privately. A report may be closed when it cannot be reproduced, is outside the repository's scope or requires testing against systems without authorization.

## Safe testing rules

- Test only with accounts, tenants and infrastructure that you own or are explicitly authorized to assess.
- Do not access another person's data, degrade service, send bulk traffic, bypass billing or retain downloaded data.
- Do not publish exploit details before a fix or mitigation is available.
- Stop testing and report immediately if real credentials or personal data are exposed.

## Exposed credentials

A credential committed to Git, included in a frontend bundle, image, log, screenshot or conversation must be treated as compromised. Revoke and replace it at the provider first; deleting the visible occurrence is not sufficient. Local, staging and production credentials must remain separate.
