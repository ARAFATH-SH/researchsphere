#!/usr/bin/env bash
# Stop this project's dev servers: the API (API_PORT, default 8080) and the Next.js dev
# server (ports 3000-3002). Run it through `make stop`.
#
# A process is stopped only when it listens on one of those ports, belongs to the current
# user and runs inside this repository (its working directory is under the repo root).
# Docker containers are never touched: their ports are not in the list, and the processes
# that publish them belong to root.
#
# Afterwards a stale frontend/.next/dev/lock is removed when no dev server is running.
#
# PORTS overrides the list of ports (space-separated), for example to test the script.

set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
lock="$root/frontend/.next/dev/lock"
ports=${PORTS:-"${API_PORT:-8080} 3000 3001 3002"}
me=$(id -u)
my_pgid=$(ps -o pgid= -p $$ | tr -d ' ')

# Print the PIDs listening on a TCP port. Without root, ss shows the PID only for our own
# processes, so other users' listeners never appear here.
listeners() {
  ss -ltnpH "sport = :$1" 2>/dev/null | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u
}

in_repo() {
  local cwd
  cwd=$(readlink "/proc/$1/cwd" 2>/dev/null) || return 1
  [[ "$cwd" == "$root" || "$cwd" == "$root"/* ]]
}

alive() { kill -0 "$1" 2>/dev/null; }

stopped=0
for port in $ports; do
  for pid in $(listeners "$port"); do
    uid=$(ps -o uid= -p "$pid" | tr -d ' ')
    name=$(ps -o args= -p "$pid")
    if [[ "$uid" != "$me" ]]; then
      echo "port $port: skipped pid $pid ($name): belongs to another user"
      continue
    fi
    if ! in_repo "$pid"; then
      echo "port $port: skipped pid $pid ($name): not running in $root"
      continue
    fi

    # Stop the whole job (make, npm or go run, and the server), as Ctrl+C would. Signal
    # only the server itself when its group is a session (a terminal shell), is our own
    # group, or is led by a process outside the repository.
    pgid=$(ps -o pgid= -p "$pid" | tr -d ' ')
    sid=$(ps -o sid= -p "$pid" | tr -d ' ')
    if [[ -n "$pgid" && "$pgid" != "$sid" && "$pgid" != "$my_pgid" ]] && in_repo "$pgid" \
      && [[ "$(ps -o uid= -p "$pgid" | tr -d ' ')" == "$me" ]]; then
      kill -TERM -- "-$pgid" 2>/dev/null
      target="process group $pgid"
    else
      kill -TERM "$pid" 2>/dev/null
      target="pid $pid"
    fi

    for _ in $(seq 50); do alive "$pid" || break; sleep 0.1; done
    if alive "$pid"; then
      kill -KILL "$pid" 2>/dev/null
      echo "port $port: killed pid $pid ($name) with SIGKILL; it ignored SIGTERM"
    else
      echo "port $port: stopped $name ($target)"
    fi
    stopped=$((stopped + 1))
  done
done

[[ $stopped -eq 0 ]] && echo "No dev servers running on ports: $ports"

# The lock records the dev server's PID. It is stale when no dev server listens on the
# Next.js ports and that PID is gone.
if [[ -f "$lock" ]]; then
  running=""
  for port in 3000 3001 3002; do
    [[ -n "$(listeners "$port")" ]] && running="$running $port"
  done
  lock_pid=$(grep -o '"pid":[0-9]*' "$lock" | cut -d: -f2)
  if [[ -n "$running" ]]; then
    echo "Kept $lock: a dev server still listens on port(s)$running"
  elif [[ -n "$lock_pid" ]] && alive "$lock_pid"; then
    echo "Kept $lock: its process $lock_pid is still running"
  else
    rm -f "$lock"
    echo "Removed stale $lock"
  fi
fi
