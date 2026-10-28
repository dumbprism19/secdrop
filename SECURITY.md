# Security policy

secdrop handles decrypted secrets, so security reports are taken seriously.

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Instead, report it
privately through GitHub: open the repository's **Security** tab and choose
**Report a vulnerability**.

Include what you found, how to reproduce it, and what an attacker could do with
it. You'll get a reply as soon as possible, and credit in the fix if you'd like.

## What counts

Anything that could expose a secret value or let someone change one without the
review step, for example:

- plaintext appearing in logs, error messages, crash output or terminal scrollback
- plaintext written to disk other than by an explicit export
- a save that bypasses the review step or overwrites a newer version
- clipboard contents that are not cleared as documented

## Supported versions

Only the latest release receives fixes.
