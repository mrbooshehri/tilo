# Agent Instructions — Tilo

## Project Overview

**Tilo** is a **Golang-based CLI log viewer** designed for DevOps and platform engineers.

The application focuses on:
- High-performance log viewing
- Powerful, configurable colorization rules
- Vim-like navigation and interaction
- Efficient searching and selection
- Seamless handling of different log formats

Tilo must work smoothly with **large log files** and **streamed input**.

---

## Core Goals

- Read logs from files or `stdin`
- Normalize line endings (CRLF → LF) automatically
- Provide syntax-aware colorization for common infrastructure patterns
- Support fast search with match counting and navigation
- Use Vim-style keybindings for navigation, search, and selection
- Allow copying selected text to the system clipboard
- Be fully configurable via a user config file

---

## Functional Requirements

### 1. Input Handling
- Accept input from:
  - File path
  - `stdin` (pipe support)
- Automatically convert **CRLF** to **LF**
- Do not modify source files

---

### 2. Colorization Engine

Tilo’s colorization is **rule-based and configurable**.

#### Default Highlight Targets
The following patterns must be highlighted by default:

- **Timestamp**  
  - ISO-8601, RFC3339, and common log formats
- **IPv4 and IPv6 addresses**
- **MAC addresses**
- **Directory / file paths** (Linux & Unix style)
- **Log levels**:
  - `INFO`, `WARN`, `ERROR`, `DEBUG`, `TRACE`, `FATAL`
- **Common DevOps / Platform keywords**:
  - `kube`, `pod`, `node`, `container`
  - `nginx`, `envoy`
  - `http`, `grpc`, `tcp`, `udp`
  - `timeout`, `retry`, `panic`, `crash`

#### Configurable Rules
- Color rules must be configurable via a **config file**
- Users can:
  - Add custom regex patterns
  - Assign colors/styles per pattern
  - Enable or disable built-in rules

Example config concept:
```yaml
colors:
  timestamp: cyan
  ip: yellow
  error: red
custom_rules:
  - pattern: "payment-service"
    color: magenta

