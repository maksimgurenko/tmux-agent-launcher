# Remote Codex sessions

Every installation can serve its own folders and launch Codex sessions on other configured machines. Add a `New Codex [hostname]` entry to the existing **Agent session** menu for each configured peer. Selecting it opens the local Rofi typeahead with folders from that peer.

## Peer configuration

Peers are listed in each machine’s launcher configuration. A peer entry gives its display name, hostname or IP address, folder-search port and authentication token reference, plus the Codex WebSocket endpoint (including port) and its token reference. The address must be reachable from the initiating machine over a LAN, Tailscale or another configured network.

Each machine that accepts folder searches configures its own listen address, port and token reference, and enables the launcher's HTTP service. A machine that accepts remote Codex sessions also needs a reachable, authenticated Codex app server. For two-way use, enable both services on each machine and list the other machine as a peer in each configuration file.

## Folder search

Provide a systemd user service that can start the folder-search listener at boot. Authenticate requests before returning directory information.

For each edit, send the current input to the host and display the results for the latest input. The service must call the same Go directory-suggestion code as the local picker. Preserve case-insensitive prefix matching, result order, dot-directory handling, Tab completion and `/` to list children. Resolve `~` against the remote user’s home. On Enter, validate the selection on the host and return its canonical absolute path. If the host is unavailable, show which host failed and let the user retry or cancel.

## Launch and reattach

Start the local Codex TUI in a launcher-managed local tmux session. Pass the configured remote WebSocket endpoint with its explicit port, the token environment-variable name, and the selected remote path via `-C`. Honor the profile’s configured launch flags, including `--yolo` when selected. The remote directory may exist only on the server.

Use the host, profile and canonical remote path to identify the live session. Selecting the same combination reattaches to it. Show the hostname in its **Attach** entry so identical paths on different machines remain distinguishable.

## Acceptance

- With two reachable machines configured as peers, launch from A into a folder unique to B, then from B into a folder unique to A. Each picker shows the destination machine’s folders and each Codex session starts in the selected folder.
- A missing or incorrect search token yields an authentication error and returns no folder names.
- Against an identical directory fixture, local and HTTP searches return the same ordered suggestions for the same input.
- A user can type ahead to a remote-only directory, select it and see Codex open in that directory on the chosen host.
- Reopening that host and directory reuses the session. Choosing another host with the same path opens a distinct session.
- Cancelling the picker creates no session, and an unavailable remote leaves local launch actions usable.
