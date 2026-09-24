# nginx vhost — chat.kayushkin.com

`chat.kayushkin.com.conf` is the vhost that puts multichat on the internet. It
is more than a proxy: it serves the built frontend from
`/home/kayushkincom/multichat-frontend` with an SPA fallback, proxies `/api/` to
the Go server on `:8402`, and gives `/ws` the WebSocket upgrade plus the 86400s
read timeout that a chat socket needs to survive an idle conversation. It was
captured byte-for-byte from what nginx was actually serving.

Until it was committed here it existed only as a hand-edit in `/etc/nginx`,
tracked by no repo — so rebuilding this box would have restored the service but
not the config that makes its chat sockets stay open.

> The frontend root is a path **outside this repo**. That is what the live vhost
> says, so that is what is committed; it is recorded here rather than quietly
> "fixed", because changing where nginx looks for the bundle is a deploy
> decision, not a transcription one.

## Installing it

```sh
./deploy/nginx/install.sh
```

Safe to run on its own: it touches nothing but nginx, and a vhost already
matching this repo is skipped without a reload. It preflights `nginx -t`, takes
a timestamped backup, rolls back if the new config fails to parse **or** fails
to serve, and only then leaves the reload in place.

**This repo has no `deploy.sh`.** Every other repo that ships a vhost calls
`install.sh` from the end of its deploy script; here there is no deploy script
to call it from, so run it by hand after changing the vhost. `install.sh` is
identical in all six vhost-owning repos (dash, kayushkin.com,
argraphments, inber-party, multichat, forge) — copy it whole, do not fork it.
