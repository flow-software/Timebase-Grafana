# Timebase Atlas data source for Grafana

A backend data source plugin for Timebase Atlas: entities, current values and relationships, with a Node Graph view and read-only Cypher. It uses only the Atlas `/api/v1` REST API (worlds, instances, relationship types, queries); history comes from Cypher `UNWIND` (flow-software/Timebase#2394 was closed in favour of it). It does not use the i3X API or the internal SignalR value hub. The user-facing documentation, which Grafana.com shows, is [src/README.md](src/README.md).

| Folder | What is there |
| --- | --- |
| `pkg/auth` | Token cache and renewal (client credentials against Pulse), retry on 401. Same code as `Historian/pkg/auth`. |
| `pkg/atlas` | Atlas REST client: worlds, instances, relationship types, Cypher. Problem-details errors are passed through. |
| `pkg/plugin` | `history.go` History query (Cypher `UNWIND` with a typed `MATCH`, `$from`/`$to` parameters, minute/hour/day buckets); `datasource.go` query types, health check, picker resources and node links; `values.go` Values query and property flattening; `graph.go` node graph frames, colours and the wide table; `tree.go` containment tree for the browse modal (cached one minute); `frames.go` shared frame helpers. Fixtures in `testdata/` are real Atlas 2.0.0 responses. |
| `src` | Config editor, query editor, browse modal (`InstanceBrowser`, `InstanceField`, `TypeField`), Cypher helpers (`cypher.ts`), tree helpers (`tree.ts`). |
| `provisioning` | Dev data source (uid `atlas-dev`) and the demo dashboard. |

Why Cypher and not `/values` for values: `/values` leaves out compound properties without a `Value` part (MaintainX `LiveAsset` on CNC01 returns nothing; flow-software/Timebase#2395), and `/instances` has no labels. Cypher returns both.

## Build

Every build step runs in a container. Go caches use the Historian's `tbi3x-*` volumes; `node_modules` lives in `tbatlas-node-modules`.

```powershell
$repo = (Resolve-Path .).Path

# Frontend: install, typecheck, lint, test and build into dist/
docker run --rm -v "${repo}:/src" -v tbatlas-node-modules:/src/node_modules -v tbi3x-npm-cache:/root/.npm -w /src node:24 `
  sh -c "npm ci && npm run typecheck && npm run lint && npm run test:ci && npm run build"

# Backend: test, then build the linux binary into dist/
docker run --rm -v "${repo}:/src" -v tbi3x-gomod:/go/pkg/mod -v tbi3x-gocache:/root/.cache/go-build -v tbi3x-gobin:/go/bin -w /src golang:1.26 `
  sh -c "go test ./... && (command -v mage >/dev/null || go install github.com/magefile/mage@v1.15.0) && mage -v build:linux"
```

`npm run build` does not type-check, so always run `npm run typecheck` too. All platforms: `sh scripts/build-backend.sh` in the same Go container. Zips: `python scripts/package.py`.

## Dev Grafana

A Grafana of its own, with the plugin mounted from `dist/`, the dev data source and the demo dashboard:

```powershell
$env:ATLAS_CLIENT_SECRET = "<Atlas client secret>"
docker compose -f docker-compose.yaml -f docker-compose.timebase.yaml up -d --build
```

It uses port 3000, so it cannot run next to the shared dev Grafana (container `flowsoftware-timebasehistorian-datasource`). That one runs in development mode, which loads unsigned plugins, and during development the plugin was copied into its data volume instead:

```powershell
docker cp dist/. flowsoftware-timebasehistorian-datasource:/var/lib/grafana/plugins/flowsoftware-timebaseatlas-datasource
docker restart flowsoftware-timebasehistorian-datasource
```

## CI

`.github/workflows/atlas-ci.yml` (at the repo root) builds, lints, tests and packages the plugin on pushes to `main` and on pull requests that touch `Atlas/`, through `atlas-build.yml`. The zips are attached to the run as the `atlas-plugin` artifact. There is no release workflow yet.

## Not done yet

- Live push: the plugin reads on panel refresh. Atlas's own LIVE switch uses the internal SignalR hub, which this plugin does not use.
- Release workflow, e2e tests, Grafana.com screenshots, signing.
