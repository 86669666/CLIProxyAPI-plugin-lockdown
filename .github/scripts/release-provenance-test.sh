#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -- "$script_dir/../.." && pwd)"
refresh_script="$script_dir/refresh-model-catalogs.sh"
provenance_script="$script_dir/write-release-provenance.sh"
pinned_commit="$(tr -d '[:space:]' < "$repository_root/.github/models.commit")"

if "$refresh_script" --validate-ref main >/dev/null 2>&1; then
	printf 'mutable branch ref was accepted\n' >&2
	exit 1
fi
if "$refresh_script" --validate-ref v1.2.3 >/dev/null 2>&1; then
	printf 'mutable tag ref was accepted\n' >&2
	exit 1
fi
if "$refresh_script" --validate-ref "${pinned_commit:0:12}" >/dev/null 2>&1; then
	printf 'short commit ref was accepted\n' >&2
	exit 1
fi
"$refresh_script" --validate-ref "$pinned_commit"

temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

create_models_repository() {
	local name="$1"
	local codex_source="${2:-}"
	local repository="$temp_dir/$name"
	mkdir -p "$repository"
	git -C "$repository" init --quiet
	git -C "$repository" config user.name 'Catalog Test'
	git -C "$repository" config user.email 'catalog-test@example.invalid'
	cp "$repository_root/internal/registry/models/models.json" "$repository/models.json"
	if [[ -n "$codex_source" ]]; then
		cp "$codex_source" "$repository/codex_client_models.json"
	fi
	git -C "$repository" add .
	GIT_AUTHOR_DATE='2026-08-31T00:00:00Z' GIT_COMMITTER_DATE='2026-08-31T00:00:00Z' \
		git -C "$repository" commit --quiet -m "$name"
	local commit
	commit="$(git -C "$repository" rev-parse HEAD)"
	if [[ ! "$commit" =~ ^[0-9a-f]{40}$ ]]; then
		printf 'fixture commit is not a full immutable SHA: %s\n' "$commit" >&2
		exit 1
	fi
	printf '%s\n' "$commit"
}

assert_refresh_fails_closed() {
	local name="$1"
	local repository="$2"
	local commit="$3"
	local catalog_dir="$temp_dir/catalog-$name"
	mkdir -p "$catalog_dir"
	printf 'original models\n' > "$catalog_dir/models.json"
	printf 'original codex\n' > "$catalog_dir/codex_client_models.json"
	if MODELS_REPOSITORY_URL="$repository" MODELS_REPOSITORY_REF="$commit" MODEL_CATALOG_DIR="$catalog_dir" \
		"$refresh_script" >"$temp_dir/$name.log" 2>&1; then
		printf 'refresh unexpectedly succeeded for %s\n' "$name" >&2
		exit 1
	fi
	[[ "$(cat "$catalog_dir/models.json")" == 'original models' ]]
	[[ "$(cat "$catalog_dir/codex_client_models.json")" == 'original codex' ]]
}

missing_repository="$temp_dir/missing-codex"
missing_commit="$(create_models_repository missing-codex)"
assert_refresh_fails_closed missing-codex "$missing_repository" "$missing_commit"

invalid_codex="$temp_dir/invalid-codex.json"
printf '{"models":[]}\n' > "$invalid_codex"
invalid_repository="$temp_dir/invalid-codex"
invalid_commit="$(create_models_repository invalid-codex "$invalid_codex")"
assert_refresh_fails_closed invalid-codex "$invalid_repository" "$invalid_commit"

complete_repository="$temp_dir/complete"
complete_commit="$(create_models_repository complete "$repository_root/internal/registry/models/codex_client_models.json")"
complete_catalog="$temp_dir/catalog-complete"
mkdir -p "$complete_catalog"
MODELS_REPOSITORY_URL="$complete_repository" MODELS_REPOSITORY_REF="$complete_commit" MODEL_CATALOG_DIR="$complete_catalog" \
	"$refresh_script"
cmp "$complete_repository/models.json" "$complete_catalog/models.json"
cmp "$complete_repository/codex_client_models.json" "$complete_catalog/codex_client_models.json"

printf 'amd64 archive\n' > "$temp_dir/CLIProxyAPI_1.0.0_linux_amd64_no-plugin.tar.gz"
printf 'arm64 archive\n' > "$temp_dir/CLIProxyAPI_1.0.0_linux_aarch64_no-plugin.tar.gz"
source_commit=8acb76156f44ccd557abd7be27e40029ff7d18a8
"$provenance_script" "$temp_dir" "$source_commit" "$pinned_commit"

[[ "$(cat "$temp_dir/SOURCE_COMMIT")" == "$source_commit" ]]
[[ "$(cat "$temp_dir/MODELS_COMMIT")" == "$pinned_commit" ]]
grep -Eq '  SOURCE_COMMIT$' "$temp_dir/checksums.txt"
grep -Eq '  MODELS_COMMIT$' "$temp_dir/checksums.txt"
[[ "$(grep -Ec 'CLIProxyAPI_.*\.tar\.gz$' "$temp_dir/checksums.txt")" -eq 2 ]]
(
	cd "$temp_dir"
	sha256sum -c checksums.txt >/dev/null
)
