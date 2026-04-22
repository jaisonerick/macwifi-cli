# macwifi-cli

[![CI](https://github.com/jaisonerick/macwifi-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/jaisonerick/macwifi-cli/actions/workflows/ci.yml)

A command-line replacement for the parts of `airport` most people
used: scan nearby Wi-Fi networks, inspect the current connection,
and read saved Keychain passwords. Works on macOS 13+, returns real
BSSIDs, no `sudo` required.

```sh
go install github.com/jaisonerick/macwifi-cli@latest

macwifi-cli scan
macwifi-cli info
macwifi-cli password "MyHomeWiFi"
```

## Why this exists

macOS 14.4 removed `/usr/libexec/airport`. `wdutil info` returns
`BSSID : <redacted>` even with `sudo`. `networksetup` doesn't list
nearby networks. The Apple-recommended path (CoreWLAN's
`scanForNetworks`) only returns real BSSIDs to apps signed with a
stable Developer ID that have Location Services permission — which
shell scripts and unsigned CLIs can't get
([Apple DTS forum thread][apple-dts]).

`macwifi-cli` is a thin wrapper around the
[`macwifi`](https://github.com/jaisonerick/macwifi) Go library, which
embeds a Developer-ID-signed and notarized helper bundle to satisfy
macOS Location Services. The first scan you run will pop the system
Location Services prompt; after that, `macwifi-cli scan` returns
real data.

[apple-dts]: https://developer.apple.com/forums/thread/718331

## Install

Requires Go 1.26+.

```sh
go install github.com/jaisonerick/macwifi-cli@latest
```

The binary lands in `$(go env GOBIN)` (or `$GOPATH/bin`). Make sure
that directory is on your `$PATH`.

## Usage

```text
macwifi-cli — Wi-Fi inspection for macOS 13+

Usage:
  macwifi-cli <command> [flags] [args]

Commands:
  scan              List nearby Wi-Fi networks.
  info              Show the network the Mac is currently connected to.
  password <ssid>   Print the saved Keychain password for an SSID.
  help              Show this message.
  version           Print version information.

Global flags:
  --json            Emit JSON instead of a human-readable table.
  --no-prompt-hint  Suppress the "macOS may prompt..." stderr hint.
```

### `scan`

```sh
macwifi-cli scan
```

```text
SSID                 BSSID              RSSI  CH   BAND   WIDTH  SEC   FLAGS
Office WiFi          aa:bb:cc:dd:ee:ff  -52   149  5GHz   80     WPA2  CS
Guest                11:22:33:44:55:66  -71   36   5GHz   80     WPA2
Conference Room      77:88:99:aa:bb:cc  -58   100  5GHz   160    WPA3
MyHomeWiFi           00:11:22:33:44:55  0     0    unknown 0     WPA2  S
```

The `FLAGS` column carries `C` (currently connected) and `S` (saved
in the preferred-networks list). Saved-but-not-visible networks show
up with `RSSI 0` and channel fields zeroed.

For machine-readable output:

```sh
macwifi-cli scan --json | jq '.[] | select(.rssi > -65)'
```

### `info`

Show only the currently connected network:

```sh
macwifi-cli info
```

```text
SSID         BSSID              RSSI  CH   BAND   WIDTH  SEC   FLAGS
Office WiFi  aa:bb:cc:dd:ee:ff  -52   149  5GHz   80     WPA2  CS
```

`macwifi-cli info --json` emits a single network object, or
`{"connected": false}` if the Mac isn't on Wi-Fi.

### `password`

Print the saved Keychain password for an SSID. macOS will pop its
Keychain access dialog the first time you run this for a given
SSID — the legacy *Always Allow* button is no longer available, so
the prompt fires every time.

```sh
macwifi-cli password "MyHomeWiFi"
```

Exit codes:

- `0` — password printed to stdout.
- `1` — error.
- `2` — no saved entry for that SSID.

JSON output:

```sh
$ macwifi-cli password "MyHomeWiFi" --json
{
  "ssid": "MyHomeWiFi",
  "found": true,
  "password": "..."
}
```

If you're scripting around this, use `--no-prompt-hint` to suppress
the friendly "macOS may prompt" message that goes to stderr.

## First run — Location Services prompt

The first time `macwifi-cli` runs `scan` or `info`, the embedded
helper bundle (`WifiScanner.app`) launches and macOS shows its
standard Location Services dialog:

> **WifiScanner** wants to use your location.

Approve it. From then on, scans return real BSSIDs without further
prompts. You can revoke the permission any time from
**System Settings → Privacy & Security → Location Services**.

If approval was missed and BSSIDs are coming back empty, open the
helper bundle once manually:

```sh
open "$TMPDIR"/macwifi-*/WifiScanner.app
```

## Limitations

- macOS 13+ on Apple Silicon. Intel Macs are not supported.
- Doesn't work from a system-wide `launchd` daemon — CoreWLAN's
  Location Services check is per-user-session.
- Not a packet capture tool; for sniffing use Wireshark or the
  Wireless Diagnostics app.
- Doesn't connect / disconnect / change networks. This is a
  read-only inspection tool.

## How it works

`macwifi-cli` calls into the [`macwifi`](https://github.com/jaisonerick/macwifi)
Go library, which embeds a Developer-ID-signed and notarized Swift
helper bundle. On first use, the helper is extracted to `$TMPDIR`
and launched via `open -W`; it dials back into the CLI on a
loopback socket and proxies CoreWLAN scans and Keychain reads back
to Go. See the [project documentation](https://jaisonerick.github.io/macwifi/how-it-works)
for the architecture in depth.

## License

MIT. See [LICENSE](LICENSE).
