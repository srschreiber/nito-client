# nito

(understand this README.md was mostly written by AI, with my guidence + stamp of approval, except for this paranthetic remark! keep that in mind while reading it)


nito is an end-to-end encrypted terminal chat client.

## Why open source?

The client is open source so that anyone can audit the code and verify our **end-to-end encryption** claims. All encryption and decryption happens exclusively in this client — the broker never sees plaintext messages, keys, or audio. You don't have to take our word for it; you can read every line of code.

## Broker

The client requires a broker to connect and route messages. You have two options:

1. **Self-host**: implement your own broker against the [wire protocol](shared/websocket_types/websocket_types.go). The broker API is intentionally simple — it routes encrypted payloads without being able to read them.
2. **Use the hosted broker** *(coming soon)*: a managed broker will be available as a paid service for those who don't want to run their own infrastructure.

## Getting started

```
make run-shell
```

On first launch you will be prompted to log in or register. Point the client at your broker URL.

## What's stopping me from building my own broker?

Nothing. Seriously.

In fact, you can use the shared module as a spec to implement the broker.

We believe the only way an E2EE system can actually be trusted is through full transparency. If you read this code, understand it, and decide to fork it and build something entirely different — that's completely fine. There is nothing we can or will do about it, nor would we want to.

The one thing we do ask is attribution, as required by the [MIT License](LICENSE). Keep the copyright notice in place and you're free to do whatever you like with the code.

## Why not make the broker open source?

I only open sourced the client to be transparent. The broker, on the other hand, doesn't need to be released for you to verify the E2EE claims because you can see that nothing (including UDP data) flows to the broker unencrypted, except metadata such as usernames, room names, who is in what room, timestamps, etc (see /shared to see exactly what the broker can/can't see). Eventually, I would like to experience what it is like to run a small side-gig where users pay a modest montly fee (3-4 dollars to keep the lights on) and where I maintain the broker and develop features. If the broker is open source, it removes the incentive of paying for a service. It's unlikely that _anyone_ will ever pay for this service, however I would like to keep this option open.

## windows

dumped from chat gpt, will format later. my findings on getting it up and running on my windows machine

🪟 Windows Setup (what actually worked)
1. Git Bash didn’t work
No gcc
CGO_ENABLED=0
Audio deps (mediadevices/malgo) failed

👉 Needed a real native toolchain

2. Installed MSYS2
Downloaded MSYS2
Opened UCRT64 shell (important)
3. Installed required packages

In UCRT64:

pacman -S --needed \
  mingw-w64-ucrt-x86_64-gcc \
  mingw-w64-ucrt-x86_64-go \
  mingw-w64-ucrt-x86_64-pkgconf \
  mingw-w64-ucrt-x86_64-opus
4. Fixed Go environment (GOROOT issue)

Set Go paths manually:

source /ucrt64/etc/profile.d/go.sh
export GOROOT=/ucrt64/lib/go
export PATH=/ucrt64/bin:$GOROOT/bin:$PATH
5. Enabled cgo
go env -w CGO_ENABLED=1
6. Fixed missing tools (step-by-step errors)

You hit these in order:

❌ pkg-config not found

→ installed:

pacman -S mingw-w64-ucrt-x86_64-pkgconf
❌ opus not found

→ installed:

pacman -S mingw-w64-ucrt-x86_64-opus
7. Ran the app
go clean -cache
go run ./shellapp/
8. Docker fix (separate issue)

MSYS2 couldn’t see Docker → added to PATH:

export PATH="/c/Program Files/Docker/Docker/resources/bin:$PATH"
🧠 Key takeaways
Git Bash alone is not enough for cgo/audio
MSYS2 UCRT64 gives:
gcc
pkg-config
native libs (opus)
Required for:
mediadevices → malgo → Windows audio (WASAPI)
🔥 Minimal mental model

To run your app on Windows you needed:

Go + CGO + GCC + pkg-config + Opus + correct shell

If you want, I can condense this into a super tight README version (10 lines) or add Linux/macOS sections.

## License

MIT — see [LICENSE](LICENSE).
