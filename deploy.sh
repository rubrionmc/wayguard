#!/bin/bash
set -e

# check git
if ! command -v git &> /dev/null; then
  echo "[x] git not installed"
  exit 1
fi

# check repo
if ! git rev-parse --is-inside-work-tree &> /dev/null; then
  echo "[x] Not a git repository"
  exit 1
fi

# check remote
echo " => Fetching remote URL: \"git config --get remote.origin.url\"..."
REMOTE_URL=$(git config --get remote.origin.url || echo "")
if [ -z "$REMOTE_URL" ]; then
  echo "[x] No remote 'origin' configured"
  exit 1
fi

# auto-detect repo info from git
OWNER=$(git config --get remote.origin.url | sed -E 's#.*/([^/]+)/([^/.]+)(\.git)?#\1#')
REPO=$(git config --get remote.origin.url | sed -E 's#.*/([^/]+)/([^/.]+)(\.git)?#\2#')

IMAGE="${OWNER}/${REPO}"
TAG="dev"

echo " => Building local dev image: ${IMAGE}:${TAG}"
docker build -t "${IMAGE}":${TAG} .

# minikube
if command -v minikube &> /dev/null; then
  echo " => Loading image into minikube..."
  minikube image load "${IMAGE}":${TAG}
fi

# kind
if command -v kind &> /dev/null; then
  echo " => Loading image into kind..."
  kind load docker-image "${IMAGE}":${TAG}
fi

echo "[+] Done Image: ${IMAGE}:${TAG}"

echo " => Applying Kubernetes deployment..."
kubectl apply -f ../k8s/wayguard.yaml