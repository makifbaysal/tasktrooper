---
name: never-trust-the-client
priority: 90
enabled: true
---
In any networked game the server (or host) is authoritative: a client sends inputs and intents, never outcomes. Every RPC and message handler on the authority validates the sender's identity and ownership, field ranges, rates and packet size before acting — movement against max speed and the elapsed time, a hit against line of sight and weapon range, a purchase against the server's own balance — and rejects or clamps the rest. Messages are versioned, and the client predicts and reconciles instead of the server believing it (game-networking).
