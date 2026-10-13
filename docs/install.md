# Install

Two ways in, same binary out.

## Curl (no Go needed)

```sh
curl -fsSL https://raw.githubusercontent.com/kubotdev/kubot/main/install.sh | sh
```

Downloads the release archive for your OS/arch from GitHub Releases,
verifies its sha256 checksum, and installs to `/usr/local/bin` (override
with `KUBOT_INSTALL_DIR`). Pin a version with `KUBOT_VERSION=v0.4.0`.

## Go toolchain

```sh
go install github.com/kubotdev/kubot/cmd/kubot@latest
```

Reports the module version via `kubot --version` (release binaries are
stamped at build; local `go build` reports `dev`).

## First connection

kubot needs a kubeconfig that can read the cluster — the same resolution
as kubectl: `--kubeconfig`, `$KUBECONFIG`, `~/.kube/config`, `--context`,
or in-cluster config. It only performs GET/LIST calls; a read-only role is
sufficient and recommended.

No cluster handy? Spin up a local one:

```sh
kind create cluster
kubot inspect
```

A healthy cluster reports `OK — no problems detected` and exit code 0.
