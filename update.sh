#!/bin/bash
set -e
# shellcheck source=src/util.sh
source ~/.bashrc
"$RK8S"/commands/update.sh "$*"