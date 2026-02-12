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
TAG="dev"

# clean up old local images
echo " => Removing old local images for ${IMAGE}:${TAG} (if any)..."
if docker image inspect "${IMAGE}:${TAG}" &> /dev/null; then
  docker rmi -f "${IMAGE}:${TAG}"
fi

echo " => Building local dev image: ${IMAGE}:${TAG}"
docker build -t "${IMAGE}:${TAG}" .

# load image into local clusters
if command -v minikube &> /dev/null; then
  echo " => Loading image into minikube..."
  minikube image load "${IMAGE}:${TAG}"
fi

if command -v kind &> /dev/null; then
  echo " => Loading image into kind..."
  kind load docker-image "${IMAGE}:${TAG}"
fi

echo "=> Start deployment in ../k8s"

(
  cd ../k8s || exit 1
  ./deploy.sh
)
wait


echo "[+] Done: ${IMAGE}:${TAG} deployed"