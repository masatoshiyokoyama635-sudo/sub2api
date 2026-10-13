#!/usr/bin/env bash
# Upgrade an existing custom Sub2API Compose deployment. No host Python or jq.
# Usage: bash upgrade-custom-with-backup.sh IMAGE_REF EXPECTED_VERSION EXPECTED_REVISION
# EXPECTED_VERSION is the exact binary version (for example 2.10.7-zz).
# A PostgreSQL archive/TOC check is structural validation, not a restore rehearsal.
# This script never restores a database or automatically rolls back migrations.
set -Eeuo pipefail
umask 077

die() { printf '%s\n' "$*" >&2; exit 1; }
[ "$#" -eq 3 ] || die 'usage: upgrade-custom-with-backup.sh IMAGE_REF EXPECTED_VERSION EXPECTED_REVISION'
image=$1
expected_version=$2
expected_revision=$3
[[ "$expected_revision" =~ ^[0-9a-f]{40}$ ]] || die 'expected revision must be a full 40-character Git SHA'
[[ "$expected_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-zz$ ]] || die 'expected version must be the exact custom binary version, such as 2.10.7-zz'
[[ "$image" =~ ^ghcr\.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools-([0-9a-f]{40})@sha256:([0-9a-f]{64})$ ]] || die 'image must use the custom GHCR full-revision tag and a sha256 digest'
[ "${BASH_REMATCH[1]}" = "$expected_revision" ] || die 'image tag and expected revision differ'
timeout=${SUB2API_HEALTH_TIMEOUT:-600}
[[ "$timeout" =~ ^[1-9][0-9]{0,3}$ ]] && [ "$timeout" -le 3600 ] || die 'SUB2API_HEALTH_TIMEOUT must be 1..3600 seconds'
for command_name in docker sha256sum mktemp cmp tar; do
  command -v "$command_name" >/dev/null || die "missing command: $command_name"
done
docker compose version >/dev/null
app=sub2api
inspect() { docker inspect -f "$1" "$2"; }
config_files=$(inspect '{{ index .Config.Labels "com.docker.compose.project.config_files" }}' "$app")
deploy_dir=$(inspect '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}' "$app")
project=$(inspect '{{ index .Config.Labels "com.docker.compose.project" }}' "$app")
[ -n "$config_files" ] && [ "$config_files" != '<no value>' ] && [ -d "$deploy_dir" ] && [ -n "$project" ] && [ "$project" != '<no value>' ] || die 'original Compose project was not found; upgrade stopped'
[[ "$config_files$deploy_dir$project" != *$'\n'* ]] || die 'multiline Compose metadata is unsupported'
[ "$(inspect '{{ index .Config.Labels "com.docker.compose.service" }}' "$app")" = sub2api ] || die 'container is not the sub2api Compose service'
[ "$(inspect '{{.State.Status}}' "$app")" = running ] || die 'Sub2API is not running; upgrade stopped'
old_container=$(inspect '{{.Id}}' "$app")
old_image=$(inspect '{{.Config.Image}}' "$app")
old_image_id=$(inspect '{{.Image}}' "$app")
cd -- "$deploy_dir"
deploy_dir=$(pwd -P)
[ -f .env ] || die 'missing .env in the original Compose working directory; upgrade stopped'
managed_override="$deploy_dir/.sub2api-image.override.yml"
managed_marker='# sub2api-custom-upgrade managed image override v1'
write_override() { printf '%s\nservices:\n  sub2api:\n    image: %s\n' "$managed_marker" "$1"; }
if [ -e "$managed_override" ] || [ -L "$managed_override" ]; then
  [ -f "$managed_override" ] && [ ! -L "$managed_override" ] || die 'managed override path is not a regular file'
  managed_image=$(sed -n '4s/^    image: //p' "$managed_override")
  [ -n "$managed_image" ] && [ "$(cat "$managed_override")" = "$(write_override "$managed_image")" ] || die 'managed override contains unknown settings; upgrade stopped'
fi

IFS=, read -r -a files <<< "$config_files"
args=()
legacy_missing=
source_files=("$deploy_dir/.env")
for ((index=0; index<${#files[@]}; index++)); do
  file=${files[index]}
  [[ "$file" = /* ]] || file="$deploy_dir/$file"
  files[index]=$file
  if [ ! -f "$file" ]; then
    # v2.10.5 deleted its final image-only override after Docker persisted its
    # path in the Compose label. Recover only that known, last, missing file.
    if [ ! -e "$file" ] && [ ! -L "$file" ] && [ "${file%/*}" = "$deploy_dir" ] && [[ "${file##*/}" =~ ^\.sub2api-v2\.10\.5\.[A-Za-z0-9]{6}\.yml$ ]] && [ "$index" -eq "$((${#files[@]} - 1))" ] && [ -z "$legacy_missing" ] && [ "$old_image" = 'ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools-ca0db1bd159a5a5282b72c8a999490099751ae38@sha256:46bc91e2f9184c7a5ad8aa0c04a639a69422a436bee1189c912b3cd1fecc1e9b' ]; then
      legacy_missing=$file
      continue
    fi
    die "missing Compose file: $file; upgrade stopped"
  fi
  if [ "$file" != "$managed_override" ]; then
    args+=(-f "$file")
    source_files+=("$file")
  fi
done
[ "${#args[@]}" -gt 0 ] || die 'no original Compose source remains'
compose() { docker compose --project-directory "$deploy_dir" --env-file "$deploy_dir/.env" -p "$project" "${args[@]}" -f "$managed_override" "$@"; }

backup_dir=
stage=
locked=0
override_written=0
override_existed=0
upgrade_started=0
lock_dir="$deploy_dir/.sub2api-upgrade.lock"
finish() {
  status=$?
  trap - EXIT
  set +e
  [ -z "$stage" ] || rm -f -- "$stage"
  if [ "$status" -ne 0 ] && [ "$override_written" -eq 1 ] && [ "$upgrade_started" -eq 0 ]; then
    if [ "$override_existed" -eq 1 ]; then
      cp -p "$backup_dir/managed-override-before.yml" "$managed_override"
    else
      rm -f -- "$managed_override"
    fi
  fi
  if [ "$locked" -eq 1 ]; then
    rm -f -- "$lock_dir/pid"
    rmdir -- "$lock_dir"
  fi
  if [ "$status" -ne 0 ] && [ -n "$backup_dir" ]; then
    printf 'upgrade stopped (exit=%s); backup retained at %s\n' "$status" "$backup_dir" >&2
    if [ "$upgrade_started" -eq 1 ]; then
      printf '%s\n' 'app recreation was attempted; the persistent image override is retained; no database rollback was attempted' >&2
    fi
  fi
  exit "$status"
}
trap finish EXIT
mkdir "$lock_dir" 2>/dev/null || die "an upgrade lock already exists: $lock_dir"
locked=1
printf '%s\n' "$$" > "$lock_dir/pid"

# Do not select the first result: multiple running postgres containers are an
# ambiguity, even if all of them carry the same Compose project label.
db=$(docker ps --no-trunc -q --filter "label=com.docker.compose.project=$project" --filter 'label=com.docker.compose.service=postgres')
[[ "$db" =~ ^[0-9a-f]{64}$ ]] || die 'the original Compose project must have exactly one running PostgreSQL container'
[ "$(inspect '{{ index .Config.Labels "com.docker.compose.project" }}' "$db")" = "$project" ] || die 'PostgreSQL belongs to another Compose project'
[ "$(inspect '{{ index .Config.Labels "com.docker.compose.service" }}' "$db")" = postgres ] || die 'resolved database is not the postgres service'
[ "$(inspect '{{.State.Status}}' "$db")" = running ] || die 'PostgreSQL is not running'

# Non-empty DATABASE_* environment values override config.yaml in Sub2API.
# Requiring them avoids guessing YAML values or backing up POSTGRES_DB when the
# actual application uses a different database. Credentials stay in containers.
db_env=': "${DATABASE_HOST:?explicit DATABASE_HOST is required}" "${DATABASE_PORT:?explicit DATABASE_PORT is required}" "${DATABASE_USER:?explicit DATABASE_USER is required}" "${DATABASE_PASSWORD:?explicit DATABASE_PASSWORD is required}" "${DATABASE_DBNAME:?explicit DATABASE_DBNAME is required}" "${DATABASE_SSLMODE:?explicit DATABASE_SSLMODE is required}"; export PGHOST="$DATABASE_HOST" PGPORT="$DATABASE_PORT" PGUSER="$DATABASE_USER" PGPASSWORD="$DATABASE_PASSWORD" PGDATABASE="$DATABASE_DBNAME" PGSSLMODE="$DATABASE_SSLMODE" PGCONNECT_TIMEOUT=15; '
db_identity_command="${db_env}"'psql -X -A -t -F "|" -v ON_ERROR_STOP=1 -w -c "SELECT current_database(), inet_server_addr()::text, inet_server_port();"'
app_database_identity=$(docker exec "$app" sh -ec "$db_identity_command")
runtime_key_command='printf "%s\000%s\000" "${JWT_SECRET-}" "${TOTP_ENCRYPTION_KEY-}" | sha256sum'
runtime_key_fingerprint=$(docker exec "$app" sh -ec "$runtime_key_command")
[[ "$runtime_key_fingerprint" =~ ^[0-9a-f]{64}[[:space:]]+-?$ ]] || die 'could not fingerprint runtime authentication/encryption keys'
[[ "$app_database_identity" != *$'\n'* ]] || die 'unexpected multiline database identity'
IFS='|' read -r database_name database_address database_port extra <<< "$app_database_identity"
[ -n "$database_name" ] && [ -n "$database_address" ] && [[ "$database_port" =~ ^[0-9]+$ ]] && [ -z "$extra" ] || die 'could not verify the application database identity'
database_addresses=$(inspect '{{range .NetworkSettings.Networks}}{{if .IPAddress}}{{println .IPAddress}}{{end}}{{if .GlobalIPv6Address}}{{println .GlobalIPv6Address}}{{end}}{{end}}' "$db")
address_matches=0
while IFS= read -r address; do
  [ "$address" != "$database_address" ] || address_matches=1
done <<< "$database_addresses"
[ "$address_matches" -eq 1 ] || die 'application DATABASE_HOST reaches a different database server; upgrade stopped'
db_local_identity=$(docker exec -e "BACKUP_DATABASE=$database_name" "$db" sh -ec 'exec psql -X -A -t -F "|" -v ON_ERROR_STOP=1 -w -U "$POSTGRES_USER" -d "$BACKUP_DATABASE" -c "SELECT current_database(), current_setting('\''port'\'');"')
[ "$db_local_identity" = "$database_name|$database_port" ] || die 'application database/port does not match the selected PostgreSQL container'
same_database() {
  [[ "$1" != *$'\n'* ]] || return 1
  IFS='|' read -r check_name check_address check_port check_extra <<< "$1"
  [ "$check_name" = "$database_name" ] && [ "$check_port" = "$database_port" ] && [ -z "$check_extra" ] || return 1
  while IFS= read -r address; do
    [ "$address" != "$check_address" ] || return 0
  done <<< "$database_addresses"
  return 1
}

mkdir -p "$deploy_dir/backups"
backup_dir=$(mktemp -d "$deploy_dir/backups/pre-v${expected_version%-zz}-$(date +%Y%m%d-%H%M%S).XXXXXX")
cp -p .env "$backup_dir/.env"
printf '%s\n' "$project" > "$backup_dir/project.txt"
printf '%s\n' "$config_files" > "$backup_dir/compose-label-before.txt"
printf '%s %s\n' "$old_image" "$old_image_id" > "$backup_dir/image-before.txt"
printf '%s\n' "$app_database_identity" > "$backup_dir/database-identity.txt"
printf '%s\n' "$runtime_key_fingerprint" > "$backup_dir/runtime-key-fingerprint.txt"
docker inspect -f '{{json .Config.Env}}' "$app" > "$backup_dir/app-environment.json"
docker inspect -f '{{json .Config.Env}}' "$db" > "$backup_dir/postgres-environment.json"
docker inspect -f '{{.Image}}' "$db" > "$backup_dir/postgres-image-before.txt"
docker inspect -f '{{json .Mounts}}' "$app" > "$backup_dir/app-mounts.json"
sha256sum -- "${source_files[@]}" > "$backup_dir/ORIGINAL_SOURCE_SHA256SUMS"
: > "$backup_dir/compose-files.txt"
for ((index=0; index<${#files[@]}; index++)); do
  file=${files[index]}
  printf '%s\n' "$file" >> "$backup_dir/compose-files.txt"
  if [ "$file" = "$legacy_missing" ]; then
    printf 'services:\n  sub2api:\n    image: %s\n' "$old_image" > "$backup_dir/compose.$((index + 1)).yml"
    printf 'reconstructed missing image-only override: %s\nimage from running container: %s\n' "$file" "$old_image" > "$backup_dir/legacy-override-recovery.txt"
  else
    cp -p "$file" "$backup_dir/compose.$((index + 1)).yml"
  fi
done
if [ -f "$managed_override" ]; then
  override_existed=1
  cp -p "$managed_override" "$backup_dir/managed-override-before.yml"
fi

data_dir=$(docker exec "$app" sh -ec 'cd "${DATA_DIR:-/app/data}"; pwd -P')
[[ "$data_dir" = /* && "$data_dir" != *$'\n'* ]] || die 'could not resolve the application data directory'
printf '%s\n' "$data_dir" > "$backup_dir/data-directory.txt"
config_path=$(docker exec "$app" sh -ec 'if [ -n "${CONFIG_FILE:-}" ]; then [ -f "$CONFIG_FILE" ] || exit 1; printf "%s\n" "$CONFIG_FILE"; else for d in "${DATA_DIR:-/app/data}" /app/data . ./config /etc/sub2api; do for ext in yaml yml; do if [ -f "$d/config.$ext" ]; then printf "%s\n" "$d/config.$ext"; exit 0; fi; done; done; fi')
if [ -n "$config_path" ]; then
  [[ "$config_path" != *$'\n'* ]] || die 'unexpected multiline configuration path'
  docker cp "$app:$config_path" "$backup_dir/config.yaml"
  printf '%s\n' "$config_path" > "$backup_dir/config-source.txt"
fi
archive_optional_directory() {
  container_path=$1
  archive_name=$2
  presence=$(docker exec "$app" sh -ec 'if [ -d "$1" ]; then printf present; elif [ -e "$1" ] || [ -L "$1" ]; then printf wrong-type; else printf absent; fi' sh "$container_path")
  case "$presence" in
    absent) return 0 ;;
    present)
      docker exec "$app" sh -ec 'tar -C "$1" -czf - .' sh "$container_path" > "$backup_dir/$archive_name"
      test -s "$backup_dir/$archive_name"
      tar -tzf "$backup_dir/$archive_name" > "$backup_dir/$archive_name.list"
      printf '%s\n' "$container_path" > "$backup_dir/$archive_name.source"
      ;;
    *) die "could not archive directory: $container_path" ;;
  esac
}
archive_optional_directory "$data_dir/mihomo-codex" mihomo-codex.tar.gz
archive_optional_directory "$data_dir/credential-worker/totp-recovery" credential-worker-totp-recovery.tar.gz
archive_optional_directory "$data_dir/totp-recovery" totp-recovery.tar.gz

docker exec "$app" sh -ec "${db_env}exec pg_dump -Fc" > "$backup_dir/database.dump.partial"
test -s "$backup_dir/database.dump.partial"
docker exec -i "$db" pg_restore --list < "$backup_dir/database.dump.partial" > "$backup_dir/database.toc"
test -s "$backup_dir/database.toc"
mv -- "$backup_dir/database.dump.partial" "$backup_dir/database.dump"
(cd "$backup_dir"; backup_files=(.env ./*); sha256sum -- "${backup_files[@]}" > SHA256SUMS)
(cd "$backup_dir" && sha256sum --check --status SHA256SUMS)
printf 'PostgreSQL archive and table of contents checked (not a restore test): %s\n' "$backup_dir/database.dump"

docker pull "$image"
image_revision=$(docker image inspect -f '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$image")
[ "$image_revision" = "$expected_revision" ] || die 'pulled image revision does not match the expected commit'
new_image_id=$(docker image inspect -f '{{.Id}}' "$image")
docker run --rm --pull never --network none --read-only --entrypoint /app/sub2api "$image" -version > "$backup_dir/image-version.stdout" 2> "$backup_dir/image-version.stderr"
version_output=$(cat "$backup_dir/image-version.stdout" "$backup_dir/image-version.stderr")
[[ "$version_output" = *"Sub2API $expected_version (commit: $expected_revision, built: "* ]] || die 'pulled binary version/revision verification failed'
sha256sum --check --status "$backup_dir/ORIGINAL_SOURCE_SHA256SUMS" || die 'original Compose or .env changed during backup; upgrade stopped'
[ "$(inspect '{{.Id}}' "$app")" = "$old_container" ] && [ "$(inspect '{{.Image}}' "$app")" = "$old_image_id" ] || die 'Sub2API container changed during backup; upgrade stopped'
stage=$(mktemp "$deploy_dir/.sub2api-image.XXXXXX.yml")
write_override "$image" > "$stage"
mv -- "$stage" "$managed_override"
stage=
override_written=1
compose config --quiet
# Check the next Compose environment using a disposable shell container before
# starting the application (and therefore before migrations can run).
candidate_identity=$(compose run -T --rm --no-deps --pull never --entrypoint sh sub2api -ec "$db_identity_command; $runtime_key_command")
same_database "${candidate_identity%%$'\n'*}" && [ "${candidate_identity#*$'\n'}" = "$runtime_key_fingerprint" ] || die 'prospective Compose database or JWT/TOTP keys differ from the running application; upgrade stopped'
sha256sum --check --status "$backup_dir/ORIGINAL_SOURCE_SHA256SUMS" || die 'original Compose or .env changed during preflight; upgrade stopped'
upgrade_started=1
compose up -d --no-deps --force-recreate --pull never sub2api

deadline=$((SECONDS + timeout))
healthy=0
last_state=
while [ "$SECONDS" -lt "$deadline" ]; do
  state=$(inspect '{{.State.Status}}' "$app")
  health=$(inspect '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$app")
  case "$state" in exited|dead|removing) die "Sub2API stopped before becoming healthy: $state" ;; esac
  [ -n "$health" ] || die 'Sub2API has no Docker healthcheck; cannot verify readiness'
  if [ "$state/$health" != "$last_state" ]; then
    printf 'waiting for Sub2API: %s/%s\n' "$state" "$health"
    last_state="$state/$health"
  fi
  if [ "$state" = running ] && [ "$health" = healthy ] && docker exec "$app" sh -ec 'exec wget -q -T 5 -O /dev/null "http://127.0.0.1:${SERVER_PORT:-8080}/health"'; then
    healthy=1
    break
  fi
  sleep 2
done
[ "$healthy" -eq 1 ] || die "Sub2API did not become healthy within $timeout seconds"
[ "$(inspect '{{.Config.Image}}' "$app")" = "$image" ] && [ "$(inspect '{{.Image}}' "$app")" = "$new_image_id" ] || die 'running container does not use the verified image'
[ "$(inspect '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$app")" = "$expected_revision" ] || die 'running container revision differs from the verified image'
docker exec "$app" /app/sub2api -version > "$backup_dir/running-version.stdout" 2> "$backup_dir/running-version.stderr"
version_output=$(cat "$backup_dir/running-version.stdout" "$backup_dir/running-version.stderr")
[[ "$version_output" = *"Sub2API $expected_version (commit: $expected_revision, built: "* ]] || die 'running binary version/revision verification failed'
same_database "$(docker exec "$app" sh -ec "$db_identity_command")" || die 'running application database identity changed'
[ "$(docker exec "$app" sh -ec "$runtime_key_command")" = "$runtime_key_fingerprint" ] || die 'running application JWT/TOTP keys changed'
printf '%s %s\n' "$image" "$new_image_id" > "$backup_dir/image-after.txt"
printf '%s\n' "$managed_override" > "$backup_dir/managed-override-path.txt"
cp -p "$managed_override" "$backup_dir/managed-override-after.yml"
(cd "$backup_dir" && sha256sum -- image-version.stdout image-version.stderr running-version.stdout running-version.stderr image-after.txt managed-override-path.txt managed-override-after.yml) > "$backup_dir/POST_UPGRADE_SHA256SUMS"
printf 'upgrade complete: version=%s revision=%s image=%s backup=%s override=%s\n' "$expected_version" "$expected_revision" "$image" "$backup_dir" "$managed_override"
