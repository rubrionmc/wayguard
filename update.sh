#!/bin/bash
set -e

# ANSI colors
BLUE='\033[34m'
RESET='\033[0m'

# helper function for colored echos
info() {
  echo -e "${BLUE} => $*${RESET}"
}

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
OWNER=$(echo "$REMOTE_URL" | sed -E 's#.*/([^/]+)/([^/.]+)(\.git)?#\1#')
REPO=$(echo "$REMOTE_URL" | sed -E 's#.*/([^/]+)/([^/.]+)(\.git)?#\2#')

IMAGE="ghcr.io/${OWNER}/${REPO}"
LOCAL_TAG="local-$(date +%s)"

# update LOCAL_DEV_TAGS env var
info "Updating LOCAL_DEV_TAGS with ${IMAGE}:${LOCAL_TAG}"
NEW_ENTRY="${IMAGE}:${LOCAL_TAG}"
if [[ -z "$LOCAL_DEV_TAGS" ]]; then
    export LOCAL_DEV_TAGS="$NEW_ENTRY"
else
    IFS=',' read -r -a TAG_ARRAY <<< "$LOCAL_DEV_TAGS"
    UPDATED=false
    for i in "${!TAG_ARRAY[@]}"; do
        if [[ "${TAG_ARRAY[$i]}" == ${IMAGE}:* ]]; then
            TAG_ARRAY[$i]="$NEW_ENTRY"
            UPDATED=true
        fi
    done
    if ! $UPDATED; then
        TAG_ARRAY+=("$NEW_ENTRY")
    fi
    LOCAL_DEV_TAGS=$(IFS=','; echo "${TAG_ARRAY[*]}")
    export LOCAL_DEV_TAGS
fi

# clean up old deployments
info "Clean up all old local images..."
docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' \
  | grep ":local-" \
  | awk '{print $2}' \
  | xargs -r docker rmi -f

info "Building local dev image: ${IMAGE}:${LOCAL_TAG}"
docker build -t "${IMAGE}:${LOCAL_TAG}" .

# Also tag as 'dev' for backwards compatibility
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
info "Start deployment in ../k8s"
(
  cd ../k8s || exit 1
  sh deploy.sh
)

# final message
echo "[+] Done: ${IMAGE}:${LOCAL_TAG} deployed"