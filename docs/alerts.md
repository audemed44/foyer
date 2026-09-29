# Alerts

![Alert settings](images/settings-alerts.png)

Foyer can send notifications through an [Apprise API](https://github.com/caronc/apprise-api)
server, which delivers to Telegram, Discord, ntfy, email, Pushover and many
more.

## Setting up

1. In Apprise, save your notification URLs under a key, say `foyer`.
2. In Foyer, open **Edit → Settings → Alerts**, set the Apprise URL to
   `http://apprise-api:8000/notify/foyer` (the address the Foyer container
   can reach), press **Send a test notification**, then **Save**.

```yaml
alerts:
  apprise_url: http://apprise-api:8000/notify/foyer   # empty turns alerts off
  tag: ""              # optional: only Apprise services with this tag
  down_after: 2        # failed checks in a row before a service is down
  services: true       # dashboard services down or unhealthy
  containers: false    # any other container crashing, restart-looping or unhealthy
  backups: true        # Kopia sources overdue, never backed up, or skipping files
  sync: true           # Syncthing folder errors
  certificates: true   # NPM certificates within the widget's warn_days
```

## What you get

Each problem sends **one** message when it starts and one when it clears
("Jellyfin is back up. After 12 min."), never a stream:

| | Checked | Needs |
|---|---|---|
| A service is down or unhealthy | Every `ping_interval`, after `down_after` failures | A `ping` or `container` on the service |
| Another container crashed (non-zero exit), restart-loops or is unhealthy | Every `ping_interval` | The Docker socket |
| A Kopia source is overdue, never backed up, or skipped files | Every 5 minutes | A [Kopia widget](widgets.md#kopia) |
| A Syncthing folder has errors | Every 5 minutes | A [Syncthing widget](widgets.md#syncthing) |
| A certificate in use is within `warn_days` of expiry | Every 5 minutes | An [NPM widget](widgets.md#nginx-proxy-manager) |

Kopia, Syncthing and NPM are only polled while alerts are on. If one of them
can't be reached, that's an alert too.

Open problems are kept in `/config/alerts.json`, so restarting Foyer
neither repeats them nor forgets to send the recovery. The settings tab
lists what's ongoing and the recent history, including messages Apprise
couldn't deliver.
