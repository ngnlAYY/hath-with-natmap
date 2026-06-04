#!/bin/sh
set -eu

target_uid="$(id -u hath)"
target_gid="$(id -g hath)"

if [ "${PUID:-}" != "" ] || [ "${PGID:-}" != "" ]; then
  target_uid="${PUID:-$target_uid}"
  target_gid="${PGID:-$target_gid}"

  case "$target_uid" in
    *[!0-9]* | "")
      echo "PUID must be a non-root numeric UID" >&2
      exit 1
      ;;
  esac
  case "$target_gid" in
    *[!0-9]* | "")
      echo "PGID must be a non-root numeric GID" >&2
      exit 1
      ;;
  esac
  if [ "$target_uid" = "0" ] || [ "$target_gid" = "0" ]; then
    echo "PUID and PGID must not be 0" >&2
    exit 1
  fi

  groupmod -o -g "$target_gid" hath
  usermod -o -u "$target_uid" -g "$target_gid" hath
fi

if [ "${SKIP_CHOWN:-}" != "1" ]; then
  if [ "$(stat -c '%u:%g' /data/hath)" != "$target_uid:$target_gid" ]; then
    find /data/hath -xdev -exec chown -h hath:hath {} \;
  fi
fi

if [ "$(stat -c '%u:%g' /run/hath-natmap)" != "$target_uid:$target_gid" ]; then
  find /run/hath-natmap -xdev -exec chown -h hath:hath {} \;
fi

exec su-exec hath:hath "$@"
