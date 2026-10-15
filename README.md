# Pokemon Sword/Shield NEX service (initial implementation)

Not deployed or verified in-game yet. Uses the shared Nextendo Go NEX stack.

Verified from Sword 1.3.2 main:
- Title: 0100ABF008968000; Shield title: 01008DB008C2C000 (protocol parity not yet checked).
- Build: A3B75BCD3311385AEED67FBEEB79CBB7BF02F471000000000000000000000000.
- Server ID: 25b8e100 at main+0x11083d8.
- Access key: d6eee09e, string at main+0x20688ec referenced at main+0x11083e4.
- SDK: NEX 4.6.8-appor; Pia 5.18.98.
- Observed endpoint: g25b8e100-lp1.s.n.srv.nintendo.net.
- Current client error: 2306-0502 after the redirected endpoint request.

Source provides account-validated authentication, secure registration, shared matchmaking,
NAT traversal, utility and ranking handlers. Game-specific requests remain to be traced.
Pia registration layout is provisional. No ZA4 capacity/identity overrides copied.
Presence publishing is deliberately not implemented until both title identities are mapped.

Next: verify Shield protocol identity; reserve unused production ports (60031/8118 are
provisional); configure TLS/SNI and account credentials; capture first login and implement
Sword/Shield-specific calls. Do not advertise online support until tested.
