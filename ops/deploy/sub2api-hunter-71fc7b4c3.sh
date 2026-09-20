#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
deploy_dir=/data/sub2api
image_ref=ghcr.io/masatoshiyokoyama635-sudo/sub2api:codex-identity-v2-team-71fc7b4c33093734cd7a38079ab01ce0fc772e45@sha256:89efbbdff09a5fc8cb6207608c6f8ca2572b9199765c2943346f8dd1cde99d74
revision=71fc7b4c33093734cd7a38079ab01ce0fc772e45
expected_version=0.2.5-zz-identity-v2-team
expected_current_version=0.2.5-zz-identity-v2-team
expected_current_revision=646f6d48f7afef1d4682477a59f8ff2daed14dc6
expected_current_digest=sha256:2d8d025b57a3401b764bb91d8dee2b725b90e1577b0024c996348bb358808bf4
cd -- "$deploy_dir"
deploy_dir=$(pwd -P)
compose_file="$deploy_dir/docker-compose.target.json"
tmp_config=
backup_dir=
switched=0
rollback_ready=0
finish() {
  status=$?
  trap - EXIT
  if test -n "$tmp_config" && ! rm -f -- "$tmp_config"; then
    test "$status" -ne 0 || status=1
  fi
  if test "$status" -ne 0 && test -n "$backup_dir"; then
    auto_rollback_status=null
    auto_rollback_attempted=false
    if test "$switched" -eq 1 && test "$rollback_ready" -eq 1; then
      auto_rollback_attempted=true
      printf 'DEPLOY_FAILED exit=%s; restoring the previous application image.\n' "$status" >&2
      if bash "$backup_dir/ROLLBACK_APP.sh" "$backup_dir" > "$backup_dir/auto-rollback.stdout.txt" 2> "$backup_dir/auto-rollback.stderr.txt"; then
        auto_rollback_status=0
        printf 'AUTO_ROLLBACK_OK backup=%s\n' "$backup_dir" >&2
      else
        auto_rollback_status=$?
        printf 'AUTO_ROLLBACK_FAILED exit=%s backup=%s; inspect auto-rollback.stderr.txt.\n' "$auto_rollback_status" "$backup_dir" >&2
      fi
    else
      printf 'DEPLOY_FAILED exit=%s backup=%s; application switch did not start.\n' "$status" "$backup_dir" >&2
    fi
    printf '%s\n' "$status" > "$backup_dir/deploy-exit-status.txt"
    printf '{"result":"FAIL","exit_status":%s,"auto_rollback":{"attempted":%s,"exit_status":%s},"database_restored":false}\n' "$status" "$auto_rollback_attempted" "$auto_rollback_status" > "$backup_dir/deployment-result.json"
  fi
  exit "$status"
}
trap finish EXIT
project=$(docker inspect sub2api --format '{{index .Config.Labels "com.docker.compose.project"}}')
test -n "$project"
test "$project" != '<no value>'
jq -e '.services.sub2api and .services.postgres and .services.redis and .services.sub2api.pull_policy == "never"' "$compose_file" >/dev/null
server_platform=$(docker info --format '{{.OSType}}/{{.Architecture}}')
case "$server_platform" in
  linux/x86_64) server_platform=linux/amd64 ;;
  linux/aarch64) server_platform=linux/arm64 ;;
  linux/amd64|linux/arm64) ;;
  *) printf 'REFUSED: unsupported Docker server platform %s.\n' "$server_platform" >&2; exit 1 ;;
esac
compose() { docker compose --project-directory "$deploy_dir" -p "$project" -f "$compose_file" "$@"; }
postgres_container=$(compose ps -q postgres)
redis_container=$(compose ps -q redis)
test -n "$postgres_container"
test -n "$redis_container"
test "$(docker inspect sub2api --format '{{.State.Status}}')" = running
test "$(docker inspect sub2api --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}')" = healthy
previous_image_id=$(docker inspect sub2api --format '{{.Image}}')
test -n "$previous_image_id"
previous_image_ref=$(docker inspect sub2api --format '{{.Config.Image}}')
test "$(docker inspect sub2api --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" = "$expected_current_revision" || { printf 'REFUSED: the current application revision is not the expected rollback baseline.\n' >&2; exit 1; }
docker image inspect "$previous_image_id" --format '{{json .RepoDigests}}' | jq -e --arg digest "$expected_current_digest" 'any(.[]?; endswith("@" + $digest))' >/dev/null || { printf 'REFUSED: the current image does not match the verified rollback digest.\n' >&2; exit 1; }
previous_version=$(curl -fsS --max-time 20 http://127.0.0.1:8080/api/v1/settings/public | jq -er '.data.version')
test "$previous_version" = "$expected_current_version" || { printf 'REFUSED: current version is %s, expected %s.\n' "$previous_version" "$expected_current_version" >&2; exit 1; }
docker pull "$image_ref"
new_image_id=$(docker image inspect "$image_ref" --format '{{.Id}}')
test "$(docker image inspect "$image_ref" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" = "$revision"
test "$(docker image inspect "$image_ref" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')" = "$expected_version"
test "$(docker image inspect "$image_ref" --format '{{.Os}}/{{.Architecture}}')" = "$server_platform"
mkdir -p backups
backup_dir=$(mktemp -d "$deploy_dir/backups/pre-identity-v2-$(date +%Y%m%d-%H%M%S)-XXXXXX")
cp -p "$compose_file" "$backup_dir/compose.json"
rollback_tag="sub2api-rollback:$(basename "$backup_dir")"
if docker image inspect "$rollback_tag" >/dev/null 2>&1; then
  printf 'REFUSED: rollback tag already exists: %s\n' "$rollback_tag" >&2
  exit 1
fi
docker image tag "$previous_image_id" "$rollback_tag"
test "$(docker image inspect "$rollback_tag" --format '{{.Id}}')" = "$previous_image_id"
printf 'Backup directory: %s\nRollback image: %s (%s)\n' "$backup_dir" "$rollback_tag" "$previous_image_id"
docker exec "$postgres_container" sh -ec 'pg_dump -Fc -U "$POSTGRES_USER" "$POSTGRES_DB"' > "$backup_dir/postgres.dump"
test -s "$backup_dir/postgres.dump"
docker exec -i "$postgres_container" pg_restore --list < "$backup_dir/postgres.dump" > "$backup_dir/postgres.toc"
test -s "$backup_dir/postgres.toc"
jq -n --arg deploy_dir "$deploy_dir" --arg project "$project" --arg previous_image_id "$previous_image_id" --arg previous_image_ref "$previous_image_ref" --arg previous_version "$previous_version" --arg rollback_tag "$rollback_tag" --arg image_ref "$image_ref" --arg revision "$revision" --arg new_image_id "$new_image_id" --arg expected_version "$expected_version" --arg postgres_container "$postgres_container" --arg redis_container "$redis_container" '{deploy_dir:$deploy_dir,project:$project,previous:{image_id:$previous_image_id,image_ref:$previous_image_ref,version:$previous_version,rollback_tag:$rollback_tag},release:{image_ref:$image_ref,image_id:$new_image_id,revision:$revision,version:$expected_version},postgres_container:$postgres_container,redis_container:$redis_container}' > "$backup_dir/deployment.json"
cat > "$backup_dir/ROLLBACK_APP.sh" <<'CODEX_IDENTITY_V2_ROLLBACK_SCRIPT'
#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
test "$#" -eq 1 || { printf 'Usage: sudo bash %s /data/sub2api/backups/BACKUP_DIRECTORY\n' "$0" >&2; exit 2; }
backup_dir=$(cd -- "$1" && pwd -P)
attempt_dir=$(mktemp -d "$backup_dir/rollback-attempt-$(date +%Y%m%d-%H%M%S)-XXXXXX")
attempt_id=$(basename "$attempt_dir")
if test -e "$backup_dir/rollback-result.json"; then
  mv -- "$backup_dir/rollback-result.json" "$attempt_dir/previous-result.json"
fi
tmp_config=
finish_rollback() {
  status=$?
  trap - EXIT
  if test -n "$tmp_config" && ! rm -f -- "$tmp_config"; then
    test "$status" -ne 0 || status=1
  fi
  if test "$status" -ne 0; then
    printf '{"attempt_id":"%s","result":"FAIL","exit_status":%s}\n' "$attempt_id" "$status" > "$attempt_dir/result.json"
  fi
  if ! cp -p "$attempt_dir/result.json" "$backup_dir/rollback-result.json"; then
    test "$status" -ne 0 || status=1
  fi
  exit "$status"
}
trap finish_rollback EXIT
printf '{"attempt_id":"%s","result":"RUNNING","exit_status":null}\n' "$attempt_id" > "$attempt_dir/result.json"
cp -p "$attempt_dir/result.json" "$backup_dir/rollback-result.json"
printf 'Rollback attempt: %s\n' "$attempt_dir"
test -f "$backup_dir/deployment.json"
(cd "$backup_dir" && sha256sum -c SHA256SUMS)
deploy_dir=$(jq -er '.deploy_dir' "$backup_dir/deployment.json")
project=$(jq -er '.project' "$backup_dir/deployment.json")
rollback_tag=$(jq -er '.previous.rollback_tag' "$backup_dir/deployment.json")
previous_image_id=$(jq -er '.previous.image_id' "$backup_dir/deployment.json")
previous_version=$(jq -er '.previous.version' "$backup_dir/deployment.json")
image_ref=$(jq -er '.release.image_ref' "$backup_dir/deployment.json")
postgres_container=$(jq -er '.postgres_container' "$backup_dir/deployment.json")
redis_container=$(jq -er '.redis_container' "$backup_dir/deployment.json")
cd -- "$deploy_dir"
compose_file="$deploy_dir/docker-compose.target.json"
compose() { docker compose --project-directory "$deploy_dir" -p "$project" -f "$compose_file" "$@"; }
test "$(docker image inspect "$rollback_tag" --format '{{.Id}}')" = "$previous_image_id"
test "$(compose ps -q postgres)" = "$postgres_container"
test "$(compose ps -q redis)" = "$redis_container"
cp -p "$compose_file" "$attempt_dir/compose.before-rollback.json"
active_image=$(jq -er '.services.sub2api.image' "$attempt_dir/compose.before-rollback.json")
test "$active_image" = "$image_ref" || test "$active_image" = "$rollback_tag"
baseline_config=$(jq -S 'del(.services.sub2api.image)' "$backup_dir/compose.json" | sha256sum)
active_config=$(jq -S 'del(.services.sub2api.image)' "$attempt_dir/compose.before-rollback.json" | sha256sum)
test "$baseline_config" = "$active_config" || { printf 'REFUSED: Compose has unrelated changes; reconcile them before rollback.\n' >&2; exit 1; }
tmp_config=$(mktemp "$deploy_dir/.docker-compose.rollback.XXXXXX")
cp -p "$backup_dir/compose.json" "$tmp_config"
jq --arg image "$rollback_tag" '.services.sub2api.image=$image' "$backup_dir/compose.json" > "$tmp_config"
docker compose --project-directory "$deploy_dir" -p "$project" -f "$tmp_config" config --quiet
cmp -s "$compose_file" "$attempt_dir/compose.before-rollback.json" || { printf 'REFUSED: Compose changed during rollback validation; current configuration was preserved.\n' >&2; exit 1; }
mv -- "$tmp_config" "$compose_file"
compose up -d --no-deps --force-recreate --pull never --wait --wait-timeout 600 sub2api
test "$(docker inspect sub2api --format '{{.Image}}')" = "$previous_image_id"
test "$(docker inspect sub2api --format '{{.State.Status}}')" = running
test "$(docker inspect sub2api --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}')" = healthy
test "$(compose ps -q postgres)" = "$postgres_container"
test "$(compose ps -q redis)" = "$redis_container"
curl -fsS --max-time 20 http://127.0.0.1:8080/health
version=$(curl -fsS --max-time 20 http://127.0.0.1:8080/api/v1/settings/public | jq -er '.data.version')
test "$version" = "$previous_version"
jq -n --arg attempt_id "$attempt_id" --arg image_id "$previous_image_id" --arg image_ref "$rollback_tag" --arg version "$version" '{attempt_id:$attempt_id,result:"PASS",exit_status:0,image_id:$image_id,image_ref:$image_ref,version:$version,services_recreated:["sub2api"],database_restored:false}' > "$attempt_dir/result.json"
printf '\nROLLBACK_OK image=%s version=%s backup=%s\n' "$previous_image_id" "$version" "$backup_dir"
CODEX_IDENTITY_V2_ROLLBACK_SCRIPT
chmod 700 "$backup_dir/ROLLBACK_APP.sh"
(cd "$backup_dir" && sha256sum compose.json postgres.dump postgres.toc deployment.json ROLLBACK_APP.sh > SHA256SUMS && sha256sum -c SHA256SUMS)
rollback_ready=1
printf 'Rollback command: sudo bash %q %q\n' "$backup_dir/ROLLBACK_APP.sh" "$backup_dir"
tmp_config=$(mktemp "$deploy_dir/.docker-compose.target.XXXXXX")
cp -p "$compose_file" "$tmp_config"
jq --arg image "$image_ref" '.services.sub2api.image=$image' "$compose_file" > "$tmp_config"
docker compose --project-directory "$deploy_dir" -p "$project" -f "$tmp_config" config --quiet
cmp -s "$compose_file" "$backup_dir/compose.json"
test "$(docker inspect sub2api --format '{{.Image}}')" = "$previous_image_id"
mv -- "$tmp_config" "$compose_file"
switched=1
compose up -d --no-deps --force-recreate --pull never --wait --wait-timeout 600 sub2api
test "$(docker inspect sub2api --format '{{.Image}}')" = "$new_image_id"
test "$(docker inspect sub2api --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" = "$revision"
test "$(docker inspect sub2api --format '{{.State.Status}}')" = running
test "$(docker inspect sub2api --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}')" = healthy
test "$(compose ps -q postgres)" = "$postgres_container"
test "$(compose ps -q redis)" = "$redis_container"
curl -fsS --max-time 20 http://127.0.0.1:8080/health
version=$(curl -fsS --max-time 20 http://127.0.0.1:8080/api/v1/settings/public | jq -er '.data.version')
test "$version" = "$expected_version"
jq -n --arg image_id "$new_image_id" --arg image_ref "$image_ref" --arg revision "$revision" --arg version "$version" '{result:"PASS",image_id:$image_id,image_ref:$image_ref,revision:$revision,version:$version,services_recreated:["sub2api"]}' > "$backup_dir/deployment-result.json"
printf '\nDEPLOY_OK revision=%s version=%s backup=%s\n' "$revision" "$version" "$backup_dir"
