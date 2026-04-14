# nito

(this README.md was mostly written by AI, with my guidence + stamp of approval. keep that in mind while reading it)


nito is an end-to-end encrypted terminal chat client.

## Why source available?

The source is publicly available so that anyone can audit the code and verify our **end-to-end encryption** claims. All encryption and decryption happens exclusively in this client — the broker never sees plaintext messages, keys, or audio. You don't have to take our word for it; you can read every line of code.

## Broker

The client requires a broker to connect and route messages. You have two options:

1. **Self-host**: implement your own broker against the [wire protocol](shared/websocket_types/websocket_types.go). The broker API is intentionally simple — it routes encrypted payloads without being able to read them.
2. **Use the hosted broker** *(coming soon)*: a managed broker will be available as a paid service for those who don't want to run their own infrastructure.

## Getting started

On first launch you will be prompted to log in or register. Point the client at your broker URL.

## macOS / Linux

Install with one command:

```sh
curl -fsSL https://raw.githubusercontent.com/srschreiber/nito-client/main/scripts/install.sh | sh
```

Then run with `nito`.

Or download manually from the [releases page](https://github.com/srschreiber/nito-client/releases):

| Platform | Binary |
|---|---|
| macOS (Apple Silicon) | `shellapp-darwin-arm64` |
| Linux (x86-64) | `shellapp-linux-amd64` |

> **Intel Mac**: pre-built binaries are not available. Build from source using the instructions below.

Make it executable and run it:

```sh
chmod +x shellapp-darwin-arm64   # or the binary for your platform
./shellapp-darwin-arm64
```

### Building from source (macOS)

Requires [Homebrew](https://brew.sh). Run the setup script once, then build normally:

```sh
bash scripts/mac-setup.sh
make run-shell
```

## Windows

Install with one command in PowerShell:

```powershell
irm https://raw.githubusercontent.com/srschreiber/nito-client/main/scripts/install.ps1 | iex
```

Then run with `nito`.

Or download `shellapp-windows-amd64.exe` manually from the [releases page](https://github.com/srschreiber/nito-client/releases).

### Building from source (Windows)

Build from source using **MSYS2 UCRT64** (Git Bash alone is not sufficient — the app requires CGO, a real GCC toolchain, and native audio/codec libraries).

1. Download and install **MSYS2** from [https://www.msys2.org](https://www.msys2.org)
2. Open the **MSYS2 UCRT64** shortcut (not MSYS, not MinGW64 — UCRT64 specifically)
3. Install git:

```sh
pacman -S git
```

4. Clone the repo and run the setup script:

```sh
git clone https://github.com/srschreiber/nito-client.git
cd nito-client
bash scripts/windows-setup.sh
```

The script installs Go, GCC, pkg-config, Opus, and RNNoise via pacman, configures the Go environment, and persists the settings to `~/.bashrc`.

After setup, run the app with:

```sh
make run-shell
```

## What's stopping me from building my own broker?

You can use the shared module as a spec to implement a compatible broker. The wire protocol is fully documented in `shared/`.

## Why isn't the broker source available?

The broker doesn't need to be released for you to verify E2EE — you can see that nothing flows to the broker unencrypted (except metadata: usernames, room names, presence, timestamps). See `shared/` for exactly what the broker can and cannot see.

## License

Source available — see [LICENSE](LICENSE). You may read and audit the code. No rights to use, copy, modify, or distribute are granted without explicit written permission.
