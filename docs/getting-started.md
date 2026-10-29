# Getting started

## Requirements

- A host that runs Docker: a Raspberry Pi with a 64-bit OS, any Linux server, or a Mac. Images are published for `linux/arm64` and `linux/amd64`.
- Network access from that host to your nodes on **port 50000** (Talos API) and to the Kubernetes API on **port 6443**.
- A **talosconfig** for the cluster. Use one with the `os:admin` role for full functionality (see [Credentials](configuration.md#credentials)).

## 1. Run the container

Add this service to a `docker-compose.yml` (a new one or an existing one):

```yaml
services:
  talos-controller:
    image: <dockerhub-user>/talos-controller:latest
    container_name: talos-controller
    restart: unless-stopped
    user: "1000:1000"                 # your user on the host: id -u / id -g
    ports:
      - "8080:8080"
    environment:
      ADMIN_USER: admin
      ADMIN_PASSWORD: ${TC_ADMIN_PASSWORD}
      SESSION_SECRET: ${TC_SESSION_SECRET}
      TZ: Europe/Stockholm
    volumes:
      - ./talos-controller/data:/data
    mem_limit: 256m
```

Put the secrets in a `.env` file next to it:

```bash
TC_ADMIN_PASSWORD=choose-a-strong-password
TC_SESSION_SECRET=<output of: openssl rand -hex 32>
```

Create the data directory **before** starting, so it is owned by your user and not by root:

```bash
mkdir -p talos-controller/data
docker compose up -d talos-controller
```

> Building from source instead? In a clone of the repository run `./start-local.sh` (Docker) or `./start-local.sh dev` (Go + Next.js dev server). Run `./start-local.sh --help` for all options.

## 2. Log in

Open `http://<host>:8080` and sign in with `ADMIN_USER` / `ADMIN_PASSWORD`. Sessions last 12 hours by default.

## 3. Connect to your cluster

On first login there is no cluster configured yet, so you land on **Settings**.

1. **talosconfig** – paste the contents of your talosconfig (usually `~/.talos/config`) or click **Choose file…**, then **Save talosconfig**. The file is validated, stored in the data directory with mode `600`, and the private key is never sent back to the browser.
2. **Context & endpoints**
   - **Context**: pick the cluster if your talosconfig contains several.
   - **Endpoints**: the IPs or hostnames of your control plane nodes (or a VIP), one per line. Leave empty to use the endpoints stored in the talosconfig. If the context has none, you must enter at least one.
   - **Nodes** (optional): only used when Kubernetes can't be reached. Normally the node list comes from Kubernetes.
   - Click **Save & test**. Each endpoint is asked for its Talos version; a green row means it answered.
3. **kubeconfig** (optional) – without one, Talos Controller fetches an admin kubeconfig through the Talos API. Upload one if your talosconfig is not `os:admin` or you want to use different Kubernetes credentials.

Then open the **Dashboard**.

## 4. Updating

```bash
docker compose pull talos-controller && docker compose up -d talos-controller
```

The running version is shown at the bottom of the sidebar. See [Image tags](configuration.md#image-tags) to pin a version.
