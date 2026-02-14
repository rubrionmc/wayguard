#!/bin/bash
set -e

info() {
  ../k8s/util/send_info.sh "$*"
}

# check git
if ! command -v git &> /dev/null; then
  echo "[x] Could not find 'git': not installed. Please install git."
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

info "Removing entry for ${IMAGE} from DEV_TAGS file..."

DEV_TAGS_FILE="../k8s/.run/DEV_TAGS"

if [ -f "$DEV_TAGS_FILE" ]; then
  temp_file=$(mktemp)

  while IFS= read -r line || [ -n "$line" ]; do
    if [[ -z "$line" || "$line" == \#* ]]; then
      echo "$line" >> "$temp_file"
      continue
    fi

    case "$line" in
      "$IMAGE:"*)
        ;;
      *)
        echo "$line" >> "$temp_file"
        ;;
    esac
  done < "$DEV_TAGS_FILE"

  mv "$temp_file" "$DEV_TAGS_FILE"
  info "Entry for ${IMAGE} removed from DEV_TAGS"
else
  info "DEV_TAGS file does not exist, creating empty file"
  mkdir -p "$(dirname "$DEV_TAGS_FILE")"
  touch "$DEV_TAGS_FILE"
fi

info "Redeploying k8s without this local image"
(
  cd ../k8s || exit 1
  sh deploy.sh
)

echo "[+] Done"