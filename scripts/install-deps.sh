#!/usr/bin/env bash
set -euo pipefail
apt-get update
xargs -a "$(dirname "$0")/deps.txt" apt-get install -y
