# Configuration

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `ADMIN_USER` | `admin` | Login user name |
| `ADMIN_PASSWORD` | – | Login password. Either this or `ADMIN_PASSWORD_BCRYPT` is required |
| `ADMIN_PASSWORD_BCRYPT` | – | bcrypt hash of the password (preferred over a plain password) |
| `SESSION_SECRET` | random | Signs session cookies. Set it, or everyone is logged out when the container restarts |
| `SESSION_TTL` | `12h` | How long a login lasts (Go duration, e.g. `24h`) |
| `COOKIE_SECURE` | `false` | Set to `true` when the UI is served over HTTPS |
| `TALOSCONFIG` | `/config/talosconfig` | Optional talosconfig file. A talosconfig uploaded on Settings takes precedence |
| `KUBECONFIG` | `/config/kubeconfig` | Optional kubeconfig file. An uploaded kubeconfig takes precedence |
| `TALOS_CONTEXT` | – | Not needed when you choose the context on Settings |
| `DATA_DIR` | `/data` | Where settings, uploaded configs and job history are stored |
| `GITHUB_TOKEN` | – | Optional. Raises the GitHub API rate limit used for release checks |
| `LISTEN_ADDR` | `:8080` | Address the web server listens on |
| `DEBUG` | – | Any value enables debug logging |

### Creating a bcrypt password hash

```bash
docker run --rm httpd:alpine htpasswd -nbB admin 'your-password' | cut -d: -f2
```

In a `docker-compose.yml` or `.env` file, write every `$` in the hash as `$$`.

## Data directory

Everything Talos Controller stores lives in `DATA_DIR` (`/data` in the container):

| File | Contents |
|---|---|
| `talosconfig` | talosconfig uploaded on Settings (mode 600) |
| `kubeconfig` | kubeconfig uploaded on Settings (mode 600) |
| `settings.json` | Selected context, endpoint and node overrides |
| `jobs.json` | History of the last 50 jobs |

The directory must be writable by the container user. Either create it yourself before the first start and run the container with `user: "<uid>:<gid>"`, or leave `user` out and give it to the image's default user: `sudo chown 65532:65532 <data-dir>`.

Back up this directory if you want to keep settings; treat it like your talosconfig, since it contains cluster credentials.

## Credentials

Talos Controller acts with the permissions of the talosconfig you give it.

| talosconfig role | What works |
|---|---|
| `os:admin` | Everything, including upgrades, reboots, config patches and fetching a kubeconfig |
| `os:operator` | Monitoring plus some operational actions such as reboots and service restarts. Config patches need `os:admin` |
| `os:reader` | Read-only monitoring. Actions fail with a permission error, views that read the machine config (Config tab, Config drift, installer images) may be unavailable, and you must upload a kubeconfig yourself |

Exactly which API calls each role allows is defined by Talos (see the RBAC page in the Talos documentation). Create a role-limited talosconfig with:

```bash
talosctl config new --roles os:reader talosconfig-reader
```

### Using files instead of uploading

You can mount files instead of using Settings:

```yaml
    volumes:
      - ./talos-controller/data:/data
      - /path/to/talosconfig:/config/talosconfig:ro
      - /path/to/kubeconfig:/config/kubeconfig:ro    # optional
```

Settings shows the source of each file ("From file" or "Uploaded here"). An upload always wins; **Remove** on Settings falls back to the mounted file.

## Image tags

A release is made by pushing a `vX.Y.Z` tag on `main`. Images on Docker Hub are tagged:

| Tag | Points to |
|---|---|
| `latest` | The newest release |
| `X.Y` (e.g. `0.3`) | The newest patch release of that minor version |
| `X.Y.Z` (e.g. `0.3.1`) | Exactly that release |

For a cluster you care about, pin a minor version (`:0.3`) to get fixes automatically without surprises.

## Behind a reverse proxy

Talos Controller holds admin credentials for your cluster, so keep it on your LAN or behind a VPN, and preferably behind HTTPS:

- Remove `ports:` and let the proxy reach `talos-controller:8080` on a shared Docker network.
- Set `COOKIE_SECURE: "true"`.
- No special headers or sub-path support are needed; serve it on its own host name (e.g. `talos.home.example`).

## Security features

- Login is limited to 5 failed attempts per 5 minutes per IP.
- Session cookies are `HttpOnly` and `SameSite=Strict`; state-changing requests must be JSON.
- Machine configs are always shown with secrets redacted.
- Reboots and upgrades require typing the node name or version to confirm.
