#!/bin/bash
set -e
# shellcheck source=src/util.sh
source ~/.bashrc

# helper function for colored echos
info() {
  "$RK8S"/util/send_info.sh "$*"
}

echo "[*] Starting local image build..."

# check git
if ! command -v git &> /dev/null; then
  echo "[x] Could not found 'git': not installed. Please install git."
  exit 1
fi

# check docker
if ! command -v docker &> /dev/null; then
  echo "[x] Could not find 'docker': not installed. Please install Docker."
  exit 1
fi

# check repo
if ! git rev-parse --is-inside-work-tree &> /dev/null; then
  echo "[x] Not a git repository"
  exit 1
fi

# check remote
REMOTE_URL=$(git config --get remote.origin.url || echo "")
if [ -z "$REMOTE_URL" ]; then
  echo "[x] No remote 'origin' configured"
  exit 1
fi

# auto-detect repo info
OWNER=$(echo "$REMOTE_URL" | sed -E 's#(git@github.com:|https://github.com/)([^/]+)/([^/.]+)(\.git)?#\2#')
REPO=$(echo "$REMOTE_URL"  | sed -E 's#(git@github.com:|https://github.com/)([^/]+)/([^/.]+)(\.git)?#\3#')

IMAGE="ghcr.io/${OWNER}/${REPO}"
LOCAL_TAG="local-$(date +%s)"

# update DEV_TAGS file
info "Updating DEV_TAGS file with ${IMAGE}:${LOCAL_TAG}"
NEW_ENTRY="${IMAGE}:${LOCAL_TAG}"
DEV_TAGS_FILE="$RK8S/.run/DEV_TAGS"

# create file if it doesn't exist
mkdir -p "$(dirname "$DEV_TAGS_FILE")"
touch "$DEV_TAGS_FILE"

# remove existing entry for this image and add new one
temp_file=$(mktemp)
ENTRY_ADDED=false

while IFS= read -r line || [ -n "$line" ]; do
  if [[ -z "$line" || "$line" == \#* ]]; then
    echo "$line" >> "$temp_file"
    continue
  fi
  if [[ "$line" == ${IMAGE}:* ]]; then
    if ! $ENTRY_ADDED; then
      echo "$NEW_ENTRY" >> "$temp_file"
      ENTRY_ADDED=true
    fi
  else
    echo "$line" >> "$temp_file"
  fi
done < "$DEV_TAGS_FILE"

if ! $ENTRY_ADDED; then
  echo "$NEW_ENTRY" >> "$temp_file"
fi

mv "$temp_file" "$DEV_TAGS_FILE"
info "DEV_TAGS file updated: $NEW_ENTRY"

# clean up old deployments
info "Clean up all old local images..."
docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' \
  | grep ":local-" \
  | awk '{print $2}' \
  | xargs -r docker rmi -f

info "Building local dev image: ${IMAGE}:${LOCAL_TAG}"
docker build -t "${IMAGE}:${LOCAL_TAG}" .

# also tag as 'dev' for backwards compatibility
docker tag "${IMAGE}:${LOCAL_TAG}" "${IMAGE}:dev"

# load image into minikube
if command -v minikube &> /dev/null; then
  info "Loading image into minikube..."
  minikube image load "${IMAGE}:${LOCAL_TAG}"
fi

# load image into kind
if command -v kind &> /dev/null; then
  info "Loading image into kind..."
  kind load docker-image "${IMAGE}:${LOCAL_TAG}"
fi

# deploy to k8s
info "Start deployment in $RK8S"
(
  cd "$RK8S" || exit 1
  sh deploy.sh
)

# final message
echo "[+] Done: ${IMAGE}:${LOCAL_TAG} deployed"