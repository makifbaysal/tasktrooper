---
title: Use the mobile app with a tunnel
description: Reach this computer from the TaskTrooper iPhone app through a Cloudflare Tunnel or ngrok, log in to your tunnel account from the app, pair one phone, and what the phone is and is not allowed to do.
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

Settings → Remote access → Tunnel: pick a provider and an address type, then
press Start tunnel. Nothing is reachable from outside until you do.

### Cloudflare Tunnel

| | Free address | My own domain |
|---|---|---|
| Account | none | a Cloudflare account with your domain on it |
| Address | `https://<random-words>.trycloudflare.com`, new on every start | `https://<subdomain you chose>.<your domain>`, stable |
| Setup | pick "Free address" and Start | log in to Cloudflare from the app, type a subdomain, Start |
| Streaming (SSE) | **not supported** by Cloudflare quick tunnels | supported |

**Free address.** Install `cloudflared` (`brew install cloudflared` on
macOS), choose Cloudflare Tunnel → Free address, press Start tunnel. The
address appears in a few seconds. Because it changes every time the tunnel
restarts, you have to pair again after a restart unless you use your own
domain. Cloudflare documents quick tunnels as a testing feature without an
uptime guarantee.

**My own domain.** There is nothing to copy from the Cloudflare dashboard:

1. Choose Cloudflare Tunnel → My own domain and press **Log in to Cloudflare**.
   TaskTrooper runs `cloudflared tunnel login`, which opens your browser. Sign
   in with the Cloudflare account that owns the domain and pick the domain
   there (if the browser does not open, the page shows the link to open
   yourself). The login is bound to that one domain.
2. The page now shows "Logged in to Cloudflare" and the domain. Type a
   subdomain; it is prefilled with `tasktrooper-<short id>`. The address is
   `https://<subdomain>.<domain>`.
3. Press Start tunnel. The first time, TaskTrooper creates a named tunnel in your
   Cloudflare account (`tasktrooper-<short id>`), points your subdomain at it
   with a DNS record, writes the tunnel's settings to a file in its own data
   folder and runs it. Later starts reuse the tunnel; a new subdomain only adds
   a DNS record. An existing DNS record with that name is never overwritten:
   you are asked to choose another subdomain.

**Log out** deletes the certificate `cloudflared` keeps at
`~/.cloudflared/cert.pem` (the file `cloudflared tunnel login` created, which
other uses of `cloudflared` share) and is refused while the tunnel runs. The
tunnel TaskTrooper created stays in your Cloudflare account and is reused if you
log in to the same domain again; delete it in Cloudflare Zero Trust if you
do not want it.

If Cloudflare does not say which domain the login is for, the page says so and
asks for the whole address (`phone.example.com`) instead of a subdomain.

### ngrok

Install ngrok (`brew install ngrok/ngrok/ngrok`) and sign up at ngrok.com for
a free account.

ngrok has no browser sign-in for its command line, so the app cannot offer a
"log in with ngrok" button. What it does instead:

1. Copy your authtoken from dashboard.ngrok.com → Your Authtoken (the page links
   there) and paste it into "ngrok authtoken", then press **Connect**. TaskTrooper
   proves the token by starting a throw-away ngrok tunnel with it for a few
   seconds; only an authtoken ngrok accepts is kept. It is stored encrypted on
   this computer and handed to ngrok through its environment, never on a command
   line. **Disconnect** forgets it.
2. **Free address.** Press Start tunnel. ngrok assigns the address itself and it
   may change between starts.
3. **A domain from my ngrok account.** Pick a domain from the list. The authtoken
   cannot list domains (ngrok's REST API wants a separate API key), so either
   paste an ngrok API key (dashboard.ngrok.com → API; optional, stored
   encrypted, used only to list your domains) and press List my domains, which
   lists everything you reserved including the free static domain, or choose
   "Another domain…" and type a domain you reserved at dashboard.ngrok.com →
   Domains. A custom domain you own works once it is attached to your ngrok
   account.
4. Press Start tunnel.

ngrok's free plan may show a browser warning page to people opening the address
in a browser; the app talks to the API directly and is not affected. The free plan
allows one agent session at a time, so stop other ngrok agents first.

### Start automatically

"Start the tunnel when TaskTrooper launches" (off by default) brings the tunnel up on launch. With a random
address this means a new address each launch; use a stable domain if you want
the phone to stay paired without thinking about it.

## Pair a phone

One phone is connected at a time. Pairing a new phone signs the previous one
out; its next request is answered with `token_revoked` and the app shows its
pairing screen again.

1. Start the tunnel. The **Pair phone** button next to Start tunnel is only
   enabled while the tunnel is connected.
2. A window opens with a QR code and the same details as text, each with a copy
   button: the computer's name, the tunnel address and a pairing code like
   `K7QD-2M9W-XH4T`, plus how long the code is still valid. The code is valid
   for five minutes and works once. When it has expired, press **New code**.
   Closing the window cancels a code that was not used. In the window you can
   also choose Access ("Can act" or "View only") and how long the pairing stays
   valid (until disconnected, 1, 7, 30 or 90 days); changing either makes a new
   code.
3. In the iPhone app choose to connect to your own computer and scan the QR
   code. If you cannot scan, type the tunnel address and the code.
4. The window confirms "<phone> is connected". Next to the buttons the page shows
   "Phone connected: <name>" with **Disconnect**, which signs the phone out
   at once. There is no list of phones.

## What the phone can do

| | View only | Can act |
|---|---|---|
| See projects, repositories, board, tasks, comments, history, agents, chats | yes | yes |
| Add comments, move and assign tasks, create tasks | no | yes |
| Chat with the product manager and other agents, answer their questions | no | yes |
| Stop, interrupt or re-run an agent run, confirm a before-deploy step | no | yes |
| Add or remove repositories and projects, connect GitHub | no | yes |

It cannot change settings, API keys, MCP servers, runners, the tunnel or the
pairing, cannot read files or run commands, and cannot reach any other
route of the local API. This is enforced on the computer: the tunnel points at
a separate port that only answers a short list of routes, and everything else is
refused before it reaches the application.

## Security notes

- **A phone that can act can make agents act.** Agents run with your own
  permissions on this computer, so a paired "Can act" phone is trusted like
  you at the keyboard. Pair only your own phone, choose "View only" if it
  should only watch, and set an expiry.
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
- The ngrok authtoken and the optional ngrok API key are stored with the
  operating system's secure storage, never in the settings file. The Cloudflare
  login is the certificate `cloudflared` itself keeps in `~/.cloudflared`
  (readable only by you); TaskTrooper reads it to find your domain and never
  shows or logs its contents.

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
| Status stays "starting" | open "Tunnel output" on the page; a refused login or a blocked network shows up there |
| Cloudflare login window never finishes | finish it in the browser, or press Cancel and try again; the login times out after 10 minutes |
| "already has a DNS record" | that subdomain exists in your Cloudflare DNS; choose another one |
| ngrok "does not accept this authtoken" | copy it again from dashboard.ngrok.com → Your Authtoken |
| The old phone shows the pairing screen | pairing a new phone disconnects the previous one |
| App says the computer is unreachable | TaskTrooper closed, computer asleep, or a random address changed: copy the new address and pair again |
| `401 token_revoked` / `token_expired` in the app | the phone was disconnected, replaced by a newer pairing, or its expiry passed; pair again |
| Pairing says the code is invalid | codes last five minutes and work once, and a newer code replaces an older one; press New code |
| Many `429` | the phone is polling too fast; the limit is 120 requests a minute per device |

See also [Data directory and security](data-and-security.md) and
[Mobile devices and store releases](mobile-releases.md).
