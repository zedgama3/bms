# Common-password list

`xato-net-10-million-passwords-100000.txt` is the 100,000 most common passwords from the
Xato.net "10 million passwords" data, as published in Daniel Miessler's SecLists
(`Passwords/Common-Credentials/xato-net-10-million-passwords-100000.txt`; older SecLists
releases called it `10-million-password-list-top-100000.txt`). One password per line, most
common first, unmodified.

- Source: https://github.com/danielmiessler/SecLists
- Commit: `60e43f4d2b0251330ff18ea4fa76aabec0515dcb`
- Fetched: 2026-10-18
- SHA-256 of the list: `1472aafa2561df5e3293aee252aee3ca660c12b399a283cf808bb01b39be388b`
- Licence: MIT, Copyright (c) 2018 Daniel Miessler. The licence text is in `LICENSE`, next to
  the file; it must stay with any copy of the list.

`identity` embeds this directory and rejects a new password that equals a line exactly
(identity.md decision 13). The list holds no personal data: it is a list of passwords, not of
accounts.
