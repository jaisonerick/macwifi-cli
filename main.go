// macwifi-cli is a small command-line tool for inspecting Wi-Fi
// networks on macOS 13+. It is a drop-in replacement for the parts of
// the deprecated `airport` CLI that most people used: scanning nearby
// networks, inspecting the current connection, and reading saved
// passwords from the Keychain.
//
// All operations are powered by the macwifi library, which embeds a
// Developer-ID-signed helper bundle to satisfy macOS Location Services.
//
// Usage:
//
//	macwifi-cli scan                # list nearby Wi-Fi networks (table)
//	macwifi-cli scan --json         # same, as JSON
//	macwifi-cli info                # show only the currently connected network
//	macwifi-cli info --json
//	macwifi-cli password "MyHomeWiFi"
//	macwifi-cli password "MyHomeWiFi" --json
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jaisonerick/macwifi"
)

const usage = `macwifi-cli — Wi-Fi inspection for macOS 13+

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

Examples:
  macwifi-cli scan
  macwifi-cli scan --json | jq '.[] | select(.rssi > -65)'
  macwifi-cli info
  macwifi-cli password "MyHomeWiFi"

Backed by the macwifi Go library: https://github.com/jaisonerick/macwifi
`

// version is overridden at build time via -ldflags. Defaults to "dev"
// for `go install`-without-tags builds.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "scan":
		err = runScan(ctx, args)
	case "info":
		err = runInfo(ctx, args)
	case "password":
		err = runPassword(ctx, args)
	case "version", "--version", "-v":
		fmt.Println("macwifi-cli", version)
		return
	case "help", "--help", "-h":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ─── scan ────────────────────────────────────────────────────────────

func runScan(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	nets, err := macwifi.Scan(ctx)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	if *asJSON {
		return writeJSON(nets)
	}
	writeNetworkTable(nets)
	return nil
}

// ─── info ────────────────────────────────────────────────────────────

func runInfo(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	nets, err := macwifi.Scan(ctx)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	current := findCurrent(nets)
	if current == nil {
		if *asJSON {
			return writeJSON(map[string]any{"connected": false})
		}
		fmt.Println("not connected to a Wi-Fi network")
		return nil
	}

	if *asJSON {
		return writeJSON(current)
	}
	writeNetworkTable([]macwifi.Network{*current})
	return nil
}

// ─── password ───────────────────────────────────────────────────────

func runPassword(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("password", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	noHint := fs.Bool("no-prompt-hint", false, "suppress Keychain prompt hint on stderr")
	timeout := fs.Duration("timeout", 60*time.Second, "max time to wait for the macOS Keychain dialog")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("password: expected exactly one SSID argument")
	}
	ssid := fs.Arg(0)

	opts := []macwifi.PasswordOption{macwifi.WithTimeout(*timeout)}
	if !*noHint {
		opts = append(opts, macwifi.OnKeychainAccess(func(ssid string) {
			fmt.Fprintf(os.Stderr, "→ macOS may prompt for Keychain access to %q\n", ssid)
		}))
	}

	pw, err := macwifi.Password(ctx, ssid, opts...)
	if err != nil {
		return fmt.Errorf("password: %w", err)
	}
	if pw == "" {
		if *asJSON {
			return writeJSON(map[string]any{"ssid": ssid, "found": false})
		}
		fmt.Fprintf(os.Stderr, "no saved password for %q\n", ssid)
		os.Exit(2)
	}

	if *asJSON {
		return writeJSON(map[string]any{"ssid": ssid, "found": true, "password": pw})
	}
	fmt.Println(pw)
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────

func findCurrent(nets []macwifi.Network) *macwifi.Network {
	for i := range nets {
		if nets[i].Current {
			return &nets[i]
		}
	}
	return nil
}

func writeNetworkTable(nets []macwifi.Network) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SSID\tBSSID\tRSSI\tCH\tBAND\tWIDTH\tSEC\tFLAGS")
	for _, n := range nets {
		flags := ""
		if n.Current {
			flags += "C"
		}
		if n.Saved {
			flags += "S"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\n",
			fallback(n.SSID, "<hidden>"),
			fallback(n.BSSID, "-"),
			n.RSSI,
			n.Channel,
			n.ChannelBand,
			n.ChannelWidth,
			n.Security,
			flags,
		)
	}
	tw.Flush()
}

func fallback(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// jsonNetwork mirrors macwifi.Network with stable, snake_case JSON
// field names. We define our own type instead of relying on the
// library's struct tags so the CLI's output format is decoupled from
// the library's internal API and won't change underneath users.
type jsonNetwork struct {
	SSID         string `json:"ssid"`
	BSSID        string `json:"bssid"`
	RSSI         int    `json:"rssi"`
	Noise        int    `json:"noise"`
	Channel      int    `json:"channel"`
	ChannelBand  string `json:"channel_band"`
	ChannelWidth int    `json:"channel_width"`
	Security     string `json:"security"`
	PHYMode      string `json:"phy_mode"`
	Current      bool   `json:"current"`
	Saved        bool   `json:"saved"`
}

func toJSONNetwork(n macwifi.Network) jsonNetwork {
	return jsonNetwork{
		SSID:         n.SSID,
		BSSID:        n.BSSID,
		RSSI:         n.RSSI,
		Noise:        n.Noise,
		Channel:      n.Channel,
		ChannelBand:  n.ChannelBand.String(),
		ChannelWidth: n.ChannelWidth,
		Security:     n.Security.String(),
		PHYMode:      n.PHYMode,
		Current:      n.Current,
		Saved:        n.Saved,
	}
}

func writeJSON(v any) error {
	switch x := v.(type) {
	case []macwifi.Network:
		out := make([]jsonNetwork, len(x))
		for i, n := range x {
			out[i] = toJSONNetwork(n)
		}
		v = out
	case *macwifi.Network:
		jn := toJSONNetwork(*x)
		v = jn
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
