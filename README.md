# nito

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

We believe the only way an E2EE system can actually be trusted is through full transparency. If you read this code, understand it, and decide to fork it and build something entirely different — that's completely fine. There is nothing we can or will do about it, nor would we want to.

The one thing we do ask is attribution, as required by the [MIT License](LICENSE). Keep the copyright notice in place and you're free to do whatever you like with the code.

## License

MIT — see [LICENSE](LICENSE).
