---
title: Use the mobile app with a tunnel
description: Reach this computer from the TaskTrooper iPhone app through a Cloudflare Tunnel or ngrok, pair a phone, and what the phone is and is not allowed to do.
---

# Use the mobile app with a tunnel

The TaskTrooper iPhone app can talk straight to the TaskTrooper running on
your computer. There is no cloud in between: you open a tunnel from this
computer to the internet, enter the tunnel's address in the app, pair once, and
from then on the phone shows your board, task status and chats, and lets you
comment, move tasks, answer agent questions and talk to the product manager.

It works while the TaskTrooper desktop app is open and the computer is awake and
online. When either is not true the app shows the computer as unreachable.

## What you need

- TaskTrooper open on the computer (local mode).
- A tunnel program on the computer: `cloudflared` (Cloudflare) **or** `ngrok`.
  Settings → Remote access checks for both and shows the install command when
  one is missing. TaskTrooper never downloads a program on its own.
- The TaskTrooper app on the phone.

## Choose a tunnel

Settings → Remote access → Tunnel: pick a provider and an address type, then press Start tunnel. Nothing is reachable from outside until you do.

### Cloudflare Tunnel

| | Free address | My own domain |
|---|---|---|
| Account | none | a Cloudflare account with your domain on it |
| Address | `https://<random-words>.trycloudflare.com`, new on every start | `https://<hostname you chose>`, stable |
| Setup | pick "Free random address" and Start | create a tunnel in Cloudflare Zero Trust, copy its token, add a public hostname |
| Streaming (SSE) | **not supported** by Cloudflare quick tunnels | supported |

**Free address.** Install `cloudflared` (`brew install cloudflared` on
macOS), choose Cloudflare Tunnel → Free address, press Start tunnel. The
address appears in a few seconds. Because it changes every time the tunnel
restarts, you have to pair again after a restart unless you use your own
domain. Cloudflare documents quick tunnels as a testing feature without an
uptime guarantee.

**My own domain.**

1. In Cloudflare Zero Trust, Networks → Tunnels, create a tunnel and copy its
   token.
2. In the tunnel's Public Hostname tab add a hostname such as
   `trooper.example.com` with service `http://127.0.0.1:<port>`, where
   `<port>` is the address the page shows under "In Cloudflare, set the
   tunnel's public hostname service to …". The port stays the same between
   launches.
3. In TaskTrooper choose Cloudflare Tunnel → My own domain, paste the token and
   the Public hostname, press Save settings, then Start tunnel. The token is stored encrypted on this computer
   and is never shown again.

### ngrok

Install ngrok (`brew install ngrok/ngrok/ngrok`) and sign up at ngrok.com for
a free account.

1. Copy your authtoken from the ngrok dashboard and paste it into "ngrok
   authtoken" on the Remote access page. It is stored encrypted on this computer and handed to ngrok through
   its environment, never on a command line.
2. Optional: claim your free static domain (`<name>.ngrok-free.app`) in the ngrok
   dashboard, choose the "My own domain" address type and enter it as Public
   hostname. With "Free address" ngrok assigns the address itself.
3. A custom domain you own works the same way once it is attached to your ngrok
   account: enter it as Public hostname.
4. Press Save settings, then Start tunnel.

ngrok's free plan may show a browser warning page to people opening the address
in a browser; the app talks to the API directly and is not affected.

### Start automatically

"Start the tunnel when TaskTrooper launches" (off by default) brings the tunnel up on launch. With a random
address this means a new address each launch; use a stable domain if you want
the phone to stay paired without thinking about it.

## Pair a phone

1. Start the tunnel, then Remote access → Pair a phone. Choose Access ("Can act"
   or "View only") and how long the pairing stays valid (until revoked, 1, 7, 30
   or 90 days), then press Show pairing code.
2. A QR code and a code like `K7QD-2M9W-XH4T` appear. They are valid for five
   minutes and work once.
3. In the iPhone app choose to connect to your own computer and scan the QR code. If
   you cannot scan, type the tunnel address and the code.
4. The phone appears under Paired phones. Rename it, change its role or Revoke it at
   any time. A revoked phone is locked out on its very next request.

## What the phone can do

| | View only | Can act |
|---|---|---|
| See projects, repositories, board, tasks, comments, history, agents, chats | yes | yes |
| Add comments, move and assign tasks, create tasks | no | yes |
| Chat with the product manager and other agents, answer their questions | no | yes |
| Stop, interrupt or re-run an agent run, confirm a before-deploy step | no | yes |
| Add or remove repositories and projects, connect GitHub | no | yes |

It cannot change settings, API keys, MCP servers, runners, the tunnel or the
devices list, cannot read files or run commands, and cannot reach any other
route of the local API. This is enforced on the computer: the tunnel points at
a separate port that only answers a short list of routes, and everything else is
refused before it reaches the application.

## Security notes

- **A phone that can act can make agents act.** Agents run with your own
  permissions on this computer, so a paired "Can act" phone is trusted like
  you at the keyboard. Pair only your own phone, prefer "View only" for a
  second device, and set an expiry.
- Anyone who knows the tunnel address can reach the port, but without a device
  token every request is refused with 401, and repeated bad tokens or bad
  pairing codes are rate limited. A pairing code is useless after five minutes
  or one use.
- Device tokens are random 256-bit values. The computer stores only their
  SHA-256 and shows a token once, at pairing. The phone keeps it in its
  Keychain.
- TLS comes from the tunnel provider, so Cloudflare or ngrok can in principle
  see the traffic they carry. Do not use a tunnel provider you do not trust
  with your task content. The app has no way around that other than a
  private network (for example Tailscale), which also works: point the app at
  that address instead.
- Remote access → Activity log lists every change made from a phone (who, which
  action, result) and every failed sign-in. Request bodies are never logged.
- Turning the tunnel off, stopping remote access or quitting TaskTrooper cuts
  the phone off at once. Devices stay paired.
- Tunnel secrets (Cloudflare token, ngrok authtoken) are stored with the
  operating system's secure storage, never in the settings file.

## Limits

- Cloudflare quick tunnels do not support streaming responses and cut idle
  connections after about 100 seconds. The app therefore sends messages and
  polls for the reply; replies still arrive, they just are not typed out live.
- Push notifications are not available over a tunnel. The app refreshes when you
  open it and checks for changes in the background as iOS allows.
- The computer must be on, awake and online, with TaskTrooper running.
- Uploading files or images from the phone is not supported yet.

## Troubleshooting

| Symptom | Check |
|---|---|
| "Program missing" | install `cloudflared` or `ngrok`; Remote access shows the command |
| Status stays "starting" | open "Tunnel output" on the page; wrong token and blocked network show up there |
| App says the computer is unreachable | TaskTrooper closed, computer asleep, or a random address changed: copy the new address and pair again |
| `401 token_revoked` / `token_expired` in the app | the device was revoked or its expiry passed; pair again |
| Pairing says the code is invalid | codes last five minutes and work once; make a new one |
| Many `429` | the phone is polling too fast; the limit is 120 requests a minute per device |

See also [Data directory and security](data-and-security.md) and
[Mobile devices and store releases](mobile-releases.md).
