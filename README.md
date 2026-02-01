# Tilo

Tilo is a fast, terminal-based log viewer for DevOps and platform engineers. It reads from files or stdin, highlights common infrastructure patterns, and provides Vim-like navigation with search, selection, and clipboard copy.

## Features

- Read from file or stdin (pipe)
- CRLF → LF normalization without modifying source files
- Rule-based, configurable colorization
- Vim-style navigation and search
- Visual selection modes (char/line/block) with clipboard copy
- Optional follow mode (`-f`)
- Toggle line numbers and wrapping

## Install

Build locally:

```bash
go build -o tilo ./cmd/tilo
```

## Usage

```bash
# View a file
./tilo /var/log/syslog

# Follow a file
./tilo -f /var/log/syslog

# Pipe input
cat /var/log/syslog | ./tilo
```

## Sample Logs

Sample logs are included for common services under `sampel/`:

- nginx, zabbix, docker, kuber, envoy
- mysql, mongo, postgres, redis
- rabbitmq, haproxy

Example:

```bash
./tilo sampel/nginx.log
```

## Keybindings

Navigation
- `j` / `k`: down / up
- `h` / `l`: left / right
- `w` / `b` / `e`: next word / previous word / end of word
- `0` / `$`: line start / line end
- `I` / `A`: line start / line end
- `g` / `G`: top / bottom

Search
- `/` search forward
- `?` search backward
- `n` / `N`: next / previous match
- `Esc`: cancel search prompt

Selection
- `v`: visual (char)
- `V`: visual line
- `Ctrl-V`: visual block
- `Esc`: exit selection
- `y`: copy selection to clipboard

View
- `L`: toggle line numbers
- `W`: toggle line wrapping
- `q`: quit

## Configuration

Tilo reads config from:
- `$XDG_CONFIG_HOME/tilo/config.yaml`
- `~/.config/tilo/config.yaml`
- `~/.tilo.yaml`

Built-in rule names you can override/disable:
- `timestamp`
- `url`
- `ipv4`
- `ipv6`
- `mac`
- `port`
- `path`
- `level_error`
- `level_warn`
- `level_info`
- `level_debug`
- `level_trace`
- `fail`
- `success`
- `keyword`

Supported color names:

| Color | Value |
| --- | --- |
| Black | `black` |
| Red | `red` |
| Green | `green` |
| Yellow | `yellow` |
| Blue | `blue` |
| Magenta | `magenta` |
| Cyan | `cyan` |
| White | `white` |
| Gray | `gray` |

Example:

```yaml
colors:
  timestamp: cyan
  ip: yellow
  level: red
custom_rules:
  - pattern: "payment-service"
    color: magenta
status_bar: bottom
line_numbers: true
```

## Built-in highlights

- Timestamps (ISO-8601/RFC3339/common syslog)
- IPv4 / IPv6
- MAC addresses
- Unix paths
- Log levels: INFO/WARN/ERROR/DEBUG/TRACE/FATAL
- Common DevOps keywords (kube, pod, node, container, nginx, envoy, http, grpc, tcp, udp, timeout, retry, panic, crash)

## License

MIT. See `LICENSE`.
