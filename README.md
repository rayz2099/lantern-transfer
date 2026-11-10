# Lantern Transfer

Lantern Transfer is a light desktop app for direct file transfers on a local network. The first release targets Windows x86_64, macOS ARM64, and Linux x86_64. An iPhone app and AirDrop compatibility are outside the 0.0.1 scope.

## Features

- Discover nearby desktop devices by hostname, or connect by IP address.
- Send files and folders after the receiver approves the transfer.
- Copy a 10-minute LAN download link for another Lantern Transfer client to paste.
- Review recent transfer metadata on this device and clear its history.
- Receive signed updates from GitHub Releases, with an install prompt.

Files move directly between devices over HTTP. There is no account, cloud upload, or relay. Use 0.0.1 only on a trusted local network: file contents and transfer metadata are not encrypted. Both devices must be on a reachable local network, and their firewalls must allow the app's inbound connection.

## Install

Download the package for your system from [Releases](https://github.com/rayz2099/lantern-transfer/releases). On Windows, the unsigned 0.0.1 installer may trigger SmartScreen. On macOS, the ad hoc signed build is not notarized and Gatekeeper may block it on another Mac. Code signing and notarization require certificates that are not available for this release.

Use the application only on a network whose other devices you trust. A copied LAN download link grants access to the selected files until it expires.
The link opens in Lantern Transfer, not a web browser. Empty folders and interrupted transfers are not preserved in this release.

The transport follows LocalSend v2 endpoint shapes over HTTP. Official LocalSend clients normally use HTTPS, so interoperability with their default configuration is outside the 0.0.1 scope.

## Development

Install Go at the version required by `go.mod`, then run:

```bash
go test ./...
go tool mygo dev
```

The app uses MyGo's native UI. It does not require a frontend build or a WebView runtime. `mygo.json` is the source of the application version and packaging targets.

## Release

Tagging `v<version>` runs the three-platform GitHub Actions build. The workflow creates a draft Release, uploads platform packages and signed update manifests, then publishes the Release only after every build passes. The repository must have a `MYGO_UPDATER_PRIVATE_KEY` Actions secret, and `mygo.json` must contain the matching `updates.publicKey`. Keep the private key outside this repository.

## License

MIT
