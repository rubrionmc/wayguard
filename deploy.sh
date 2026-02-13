#!/bin/bash
set -e

# check git
if ! command -v git &> /dev/null; then
  echo "[x] Could not found 'git': not installed Please install git."
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

# use timestamp-based tag for local dev to force image update
LOCAL_TAG="local-$(date +%s)"
export LOCAL_DEV_TAG="$LOCAL_TAG"

# clean up old deployments
echo " => Clean up all old local images..."
docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' \
  | grep ":local-" \
  | awk '{print $2}' \
  | xargs -r docker rmi -f


echo " => Building local dev image: ${IMAGE}:${LOCAL_TAG}"
docker build -t "${IMAGE}:${LOCAL_TAG}" .

# Also tag as 'dev' for backwards compatibility
docker tag "${IMAGE}:${LOCAL_TAG}" "${IMAGE}:dev"

# load image into minikube
if command -v minikube &> /dev/null; then
  echo " => Loading image into minikube..."
  minikube image load "${IMAGE}:${LOCAL_TAG}"
fi

# load image into kind
if command -v kind &> /dev/null; then
  echo " => Loading image into kind..."
  kind load docker-image "${IMAGE}:${LOCAL_TAG}"
fi

echo " => Start deployment in ../k8s"
( cd ../k8s || exit 1
  ./deploy.sh )
wait

echo "[+] Done: ${IMAGE}:${LOCAL_TAG} deployed"