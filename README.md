# Luzifer / elastic\_cron

This project is a quick and dirty replacement for running a cron daemon inside docker containers while logging into an elasticsearch instance.

The code is basically a fork of the [rsyslog\_cron](https://github.com/Luzifer/rsyslog_cron) repo modified to log into elasticsearch.

## Advantages

- It logs the output of the jobs into an elasticsearch instance
- Crons can be started on seconds, not only on minutes like a conventional cron
- Due to the logs cron jobs can get debugged
- On success and failure a HTTP ping to [Healthchecks](https://healthchecks.io/) or [Cronitor](https://cronitor.io/) can be executed

## Usage

1. Put the [binary](https://github.com/Luzifer/elastic_cron/releases/latest) into your container
2. Generate a YAML file containing the cron definition
3. Watch your crons get executed in your log stream

## Config format

```yaml
---

elasticsearch:
  index: 'elastic_cron-%{+YYYY.MM.dd}'
  servers:
    - http://localhost:9200
  auth: [username, password]

jobs:
  - name: date
    schedule: "0 * * * * *"
    cmd: "/bin/date"
    args:
      - "+%+"
    ping_success: "https://..."
    ping_failure: "https://..."

...
```

- `elasticsearch`
  - `index` - Name of the index to write messages to (understands same date specifier as ES beats)
  - `servers` - List of elasticsearch instances of the same cluster to log to
  - `auth` - List consisting of two elements: username and password
- `schedule` - consists of 6 instead of the normal 5 fields:

```
field         allowed values
-----         --------------
second        0-59
minute        0-59
hour          0-23
day of month  1-31
month         1-12 (or names, see below)
day of week   0-7 (0 or 7 is Sun, or use names)
```

Standard format for crontab entries is supported. (See `man 5 crontab`)
