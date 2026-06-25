# Security Policy

## Reporting a Vulnerability

Please do not disclose security issues publicly before a fix is available.

Open a private security advisory if the hosting platform supports it, or contact
the maintainer through a private channel with:

- affected version or commit;
- reproduction steps;
- expected and actual behavior;
- any relevant logs with secrets removed.

## Sensitive Data

Do not include real Notion cookies, `token_v2`, API keys, admin tokens,
database files, browser storage states, or proxy credentials in issues, pull
requests, logs, screenshots, or sample configuration.

If any credential was committed or shared by mistake, revoke or rotate it
immediately. Removing it from the latest commit is not enough if it has already
been pushed.
