# Pokémon Sword / Shield NEX server

Nextendo Network server for Pokémon Sword and Pokémon Shield 1.3.2 (Y-Comm: link trades, link
battles, Surprise Trade, Max Raid Battles, stamps). Uses the shared Nextendo Go NEX stack.

Both games use the same game server: Sword `0100ABF008968000` and Shield `01008DB008C2C000` share
server ID `25b8e100` (main+0x11083d8 / +0x1108408) and access key `d6eee09e`, NEX 4.6.8, Pia 5.18.98.
Endpoint: `g25b8e100-lp1.s.n.srv.nintendo.net`.

## Game-specific handlers

Every layout below was taken from the 1.3.2 `main` (request builders and response decoders); the
Utility and Subscription handlers were confirmed in game with two accounts.

- `utility.go` — Utility (0x6E) method 9: one u8 in, one u64 out. Unanswered it shows 2306-0103.
- `subscription.go` — Subscription (0x75), which drives the Y-Comm:
  - 1/2/3 publish the player's `SubscriptionData` (512-byte profile, raid details when hosting).
  - 15/16 exchange location records (other players in the Wild Area); 17 fetches friends' records.
  - New stamps reach other players through a pushed NotificationEvent of type **112002**; the game
    then calls method 10, answered with `List<ActivePlayerSubscriptionData>`.
- `regulation.go` — MatchmakeExtension (0x6D) method 55, the Battle Stadium rule set query:
  answered with an empty `List<RegulationIndex>` (no official rule sets exist any more). Without it
  the game spins on "Checking the data…".

## Status

Working: login, link trade, matchmaking and NAT traversal, Wild Area presence, Y-Comm stamps and
Max Raid Battle invites. Battle Stadium online competitions list no rule sets.

## Disclaimer

This server ships **no** Nintendo code, keys, or copyrighted assets. It is an independent
reimplementation for use with a community-run replacement service, not affiliated with, endorsed by,
or associated with Nintendo or the game's publisher.

## License

Released under the **[PolyForm Shield License 1.0.0](LICENSE.md)**, source-available: read, use,
modify, and self-host, but do not use it to provide a product that competes with Nextendo Network.
