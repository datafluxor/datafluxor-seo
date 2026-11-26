# Security

Please report security problems privately through GitHub:
**Security → Report a vulnerability** on this repository. Do not open a public issue.

We aim to reply within a few days. Include the version (`datafluxor-seo -version`), what an attacker can do and how to reproduce it.

## How the app protects your install

- The first account needs a one-time setup code printed in the server log.
- Passwords are stored with bcrypt; sessions are random tokens kept as hashes, in an HttpOnly cookie.
- Changes need a same-site request header, so other websites cannot act for you.
- Sign-in is rate-limited per address.
- The database (with your API key and Google token) is readable by its owner only, and the key is never sent back to the browser.
- The audit crawler refuses private network addresses unless `DFSEO_CRAWL_ALLOW_PRIVATE=true`.
