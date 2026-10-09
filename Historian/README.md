# Timebase i3X data source for Grafana

A backend data source plugin that queries the Timebase Historian's i3X API. The user-facing documentation, which Grafana.com shows, is [src/README.md](src/README.md).

## Layout

| Path | What it is |
| --- | --- |
| `pkg/i3x` | i3X client: token cache and renewal (`token.go`), API calls with a retry on 401 (`client.go`), history to data frame conversion (`frames.go`). |
| `pkg/plugin` | The Grafana data source: queries, health check, and the `objects` resource behind the tag picker. |
| `pkg/models` | Data source settings and validation. |
| `src` | Frontend: config editor, query editor, template variable handling. |
| `provisioning` | Datasource provisioned into the dev Grafana. |

## Developing without installing Go or Node

Every build step runs in a container. `node_modules` and the Go caches live in Docker volumes (`tbi3x-*`), so they stay out of this folder and out of OneDrive.

```powershell
$repo = (Get-Location).Path

# Frontend: install, check, test, build into dist/
docker run --rm -v "${repo}:/src" -v tbi3x-node-modules:/src/node_modules -v tbi3x-npm-cache:/root/.npm -w /src node:24 `
  sh -c "npm install && npm run typecheck && npm run lint && npm run test:ci && npm run build"

# Backend: test, then build the linux binary into dist/
docker run --rm -v "${repo}:/src" -v tbi3x-gomod:/go/pkg/mod -v tbi3x-gocache:/root/.cache/go-build -v tbi3x-gobin:/go/bin -w /src golang:1.26 `
  sh -c "go test ./... && (command -v mage >/dev/null || go install github.com/magefile/mage@v1.15.0) && mage -v build:linux"
```

For a release build of every platform, run `sh scripts/build-backend.sh` in the same container. It builds the targets one at a time, because the parallel `mage -v` intermittently fails to write the Linux binary when the repo is a Windows folder mounted into Docker.

### Dev Grafana

The dev Grafana runs Grafana 13.1 at http://localhost:3000 (admin/admin) with the plugin mounted from `dist/` and a provisioned data source. Adding `docker-compose.timebase.yaml` joins it to the Timebase stack's network (default `timebase_2-0-rc_default`; set `TIMEBASE_NETWORK` to override), so the provisioned URLs use the container names `historian` and `pulse`. CI uses `docker-compose.yaml` alone.

Pass the client secret as an environment variable for the one command, so it is never written to a file:

```powershell
$env:TIMEBASE_CLIENT_SECRET = "<Historian client secret>"
docker compose -f docker-compose.yaml -f docker-compose.timebase.yaml up -d --build
Remove-Item Env:TIMEBASE_CLIENT_SECRET
```

To rebuild the backend, stop the dev Grafana first (`docker stop flowsoftware-timebasei3x-datasource`), build, then `docker start` it. While it runs, Grafana holds `dist/gpx_timebasei3x_linux_amd64` open and, on a Windows bind mount, the build fails to overwrite it with `cannot allocate memory`. The frontend is picked up on a browser refresh.

To package shareable zips (with `INSTALL.md` inside each), run `scripts/package.py` after a full build; see the command at the top of that file.

## Publishing

Before submitting to the Grafana plugin catalog:

1. The plugin ID prefix (`flowsoftware`) must match the Grafana.com organisation slug that signs it.
2. Add screenshots to `src/img` and list them in `src/plugin.json`.
3. Build all platforms, sign with `npm run sign` (needs a `GRAFANA_ACCESS_POLICY_TOKEN`), and run the [plugin validator](https://github.com/grafana/plugin-validator). The workflow in `.github/workflows/release.yml` does this on a version tag.
