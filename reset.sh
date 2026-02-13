#!/bin/bash
set -e

info() {
  ../k8s/util/send_info.sh "$*"
}

if ! command -v git &> /dev/null; then
  echo "[x] Could not find 'git': not installed. Please install git."
  exit 1
fi

REMOTE_URL=$(git config --get remote.origin.url || echo "")
if [ -z "$REMOTE_URL" ]; then
  echo "[x] No remote 'origin' configured"
  exit 1
fi

OWNER=$(echo "$REMOTE_URL" | sed -E 's#.*/([^/]+)/([^/.]+)(\.git)?#\1#')
REPO=$(echo "$REMOTE_URL" | sed -E 's#.*/([^/]+)/([^/.]+)(\.git)?#\2#')
IMAGE="ghcr.io/${OWNER}/${REPO}"

CURRENT_TAGS="$LOCAL_DEV_TAGS"
info "Removing LOCAL_DEV_TAGS entry for ${IMAGE} from ${CURRENT_TAGS:-"(not set)"}..."

process_tags() {
    local tags="$1"
    local result=""

    if [[ -n "$tags" ]]; then
        IFS=',' read -r -a TAG_ARRAY <<< "$tags"
        NEW_TAGS=()
        for t in "${TAG_ARRAY[@]}"; do
            if [[ "$t" != ${IMAGE}:* ]]; then
                NEW_TAGS+=("$t")
            fi
        done

        if [ ${#NEW_TAGS[@]} -gt 0 ]; then
            result=$(IFS=','; echo "${NEW_TAGS[*]}")
        fi
    fi
    echo "$result"
}

NEW_TAGS_STRING=$(process_tags "$CURRENT_TAGS")

BASHRC="$HOME/.bashrc"
if [ -f "$BASHRC" ]; then
    BASH_TAGS=""
    if grep -q "^export LOCAL_DEV_TAGS=" "$BASHRC"; then
        BASH_TAGS=$(grep "^export LOCAL_DEV_TAGS=" "$BASHRC" | sed 's/^export LOCAL_DEV_TAGS=//; s/^"//; s/"$//')
    fi

    BASH_NEW_TAGS=$(process_tags "$BASH_TAGS")

    if grep -q "^export LOCAL_DEV_TAGS=" "$BASHRC"; then
        if [[ -n "$BASH_NEW_TAGS" ]]; then
            sed -i.bak "s|^export LOCAL_DEV_TAGS=.*|export LOCAL_DEV_TAGS=\"$BASH_NEW_TAGS\"|" "$BASHRC"
            info "LOCAL_DEV_TAGS updated in $BASHRC: $BASH_NEW_TAGS"
        else
            sed -i.bak "/^export LOCAL_DEV_TAGS=/d" "$BASHRC"
            info "LOCAL_DEV_TAGS line removed from $BASHRC"
        fi
    elif [[ -n "$BASH_NEW_TAGS" ]]; then
        echo "export LOCAL_DEV_TAGS=\"$BASH_NEW_TAGS\"" >> "$BASHRC"
        info "LOCAL_DEV_TAGS added to $BASHRC: $BASH_NEW_TAGS"
    fi
fi

FISH_CONF="$HOME/.config/fish/config.fish"
if [ -f "$FISH_CONF" ]; then
    FISH_TAGS=""
    if grep -q "^set -x LOCAL_DEV_TAGS" "$FISH_CONF"; then
        FISH_TAGS=$(grep "^set -x LOCAL_DEV_TAGS" "$FISH_CONF" | sed 's/^set -x LOCAL_DEV_TAGS //; s/^"//; s/"$//')
    fi

    FISH_NEW_TAGS=$(process_tags "$FISH_TAGS")

    if grep -q "^set -x LOCAL_DEV_TAGS" "$FISH_CONF"; then
        if [[ -n "$FISH_NEW_TAGS" ]]; then
            sed -i.bak "s|^set -x LOCAL_DEV_TAGS.*|set -x LOCAL_DEV_TAGS \"$FISH_NEW_TAGS\"|" "$FISH_CONF"
            info "LOCAL_DEV_TAGS updated in $FISH_CONF: $FISH_NEW_TAGS"
        else
            sed -i.bak "/^set -x LOCAL_DEV_TAGS/d" "$FISH_CONF"
            info "LOCAL_DEV_TAGS line removed from $FISH_CONF"
        fi
    elif [[ -n "$FISH_NEW_TAGS" ]]; then
        echo "set -x LOCAL_DEV_TAGS \"$FISH_NEW_TAGS\"" >> "$FISH_CONF"
        info "LOCAL_DEV_TAGS added to $FISH_CONF: $FISH_NEW_TAGS"
    fi
fi

if [[ "$0" != "$BASH_SOURCE" ]]; then
    if [[ -n "$NEW_TAGS_STRING" ]]; then
        export LOCAL_DEV_TAGS="$NEW_TAGS_STRING"
        info "LOCAL_DEV_TAGS updated in current shell: $NEW_TAGS_STRING"
    else
        unset LOCAL_DEV_TAGS
        info "LOCAL_DEV_TAGS unset from current shell"
    fi
fi

info "Redeploying k8s without this local image"
(
  cd ../k8s || exit 1
  sh deploy.sh
)

echo "[+] Done"