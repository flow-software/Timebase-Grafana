# Installing the Timebase i3X data source (test build)

This is an **unsigned test build**. Grafana only loads unsigned plugins that are named in its `allow_loading_unsigned_plugins` setting, so every install below has two parts: put the plugin folder in place, then allow it.

Grafana Cloud does not load unsigned plugins; use a self-hosted Grafana.

## Which zip

| Zip | Use it for |
| --- | --- |
| `…windows_amd64.zip` | Grafana installed on Windows |
| `…linux_amd64.zip` | Grafana in Docker on a normal PC or server, or Grafana on Linux |
| `…linux_arm64.zip`, `…linux_arm.zip` | ARM hardware (newer ARM servers, Raspberry Pi) |
| `…darwin_amd64.zip`, `…darwin_arm64.zip` | macOS on Intel / Apple Silicon |
| `…any.zip` | Works everywhere; larger because it holds every platform |

Every zip contains one folder, `flowsoftware-timebasehistorian-datasource`. Keep that folder as it is: Grafana expects each plugin in its own folder inside the plugins directory.

## Grafana on Windows

1. Before unzipping, right-click the zip, choose **Properties** and tick **Unblock** if it is shown. Otherwise Windows may stop Grafana from starting the plugin.
2. Unzip into Grafana's plugins folder, so you end up with:
   `C:\Program Files\GrafanaLabs\grafana\data\plugins\flowsoftware-timebasehistorian-datasource\plugin.json`
3. Open `C:\Program Files\GrafanaLabs\grafana\conf\custom.ini` (create it by copying `sample.ini` if it does not exist; do not edit `defaults.ini`) and add:
   ```ini
   [plugins]
   allow_loading_unsigned_plugins = flowsoftware-timebasehistorian-datasource
   ```
   If the file already has a `[plugins]` section, add the line there instead. If `allow_loading_unsigned_plugins` is already set, add the ID to the existing value, comma-separated.
4. Restart the **Grafana** service (Services app, or `Restart-Service grafana` in an administrator PowerShell).

## Grafana in Docker

Grafana's image loads plugins from `/var/lib/grafana/plugins` inside the container. Unzip the plugin on the host and mount that folder there.

1. Unzip `…linux_amd64.zip` into a `plugins` folder next to your compose file, so you have
   `./plugins/flowsoftware-timebasehistorian-datasource/plugin.json`.
2. Mount it and allow the plugin:
   ```yaml
   services:
     grafana:
       image: grafana/grafana:latest
       ports:
         - "3000:3000"
       volumes:
         - ./plugins:/var/lib/grafana/plugins
       environment:
         GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS: flowsoftware-timebasehistorian-datasource
   ```
   Or with `docker run`:
   ```sh
   docker run -d -p 3000:3000 -v "$PWD/plugins:/var/lib/grafana/plugins" \
     -e GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=flowsoftware-timebasehistorian-datasource \
     grafana/grafana:latest
   ```
3. If you already keep Grafana's data in a volume mounted at `/var/lib/grafana`, unzip into that volume's `plugins` folder instead and only add the environment variable.
4. Restart the container after adding or updating the plugin.

**Reaching the historian from Docker:** inside a container, `localhost` is the container itself. If the Timebase Historian runs on the same machine, use `https://host.docker.internal:4512/i3x/v1`, or attach Grafana to the Timebase stack's Docker network and use `https://historian:4512/i3x/v1`.

## Check that it loaded

In Grafana go to **Connections → Data sources → Add data source** and search for **Timebase Historian**. If it is missing, look in the Grafana log for `flowsoftware-timebasehistorian-datasource`:

- `plugin is unsigned` without `Permitting unsigned plugin` means the allow-list setting was not picked up. Check the spelling, and on Windows that you edited `custom.ini` and restarted the service.
- No mention at all means Grafana did not find the folder. Check that `plugin.json` sits one folder below the plugins directory, not two.

## Configure

| Setting | Value for Timebase |
| --- | --- |
| API | **Timebase** (native: adds units and descriptions) or **i3X** (the standard API) |
| URL | Timebase: `https://<historian-host>:4512`. i3X: `https://<historian-host>:4512/i3x/v1`. Switching the API adjusts it. |
| Authentication | Client credentials |
| Token URL | `https://<pulse-host>:4542/auth/token` |
| Client ID / secret | A client registered in Timebase Pulse |
| Skip certificate verification | On, if Timebase uses its self-generated certificates |

Select **Save & test**. It should report *Connected to the Timebase Historian …*. Tokens are renewed automatically, so nothing needs re-entering when they expire.
