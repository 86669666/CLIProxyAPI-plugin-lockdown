#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -- "$script_dir/../.." && pwd)"
pinned_ref_file="$repository_root/.github/models.commit"

validate_commit_ref() {
	local ref="$1"
	if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
		printf 'models ref must be an immutable lowercase 40-character commit SHA, got %q\n' "$ref" >&2
		return 1
	fi
}

if [[ "${1:-}" == "--validate-ref" ]]; then
	if [[ $# -ne 2 ]]; then
		printf 'usage: %s --validate-ref <40-character-commit-sha>\n' "$0" >&2
		exit 2
	fi
	validate_commit_ref "$2"
	exit 0
fi
if [[ $# -ne 0 ]]; then
	printf 'usage: %s [--validate-ref <40-character-commit-sha>]\n' "$0" >&2
	exit 2
fi

if [[ ! -f "$pinned_ref_file" ]]; then
	printf 'pinned models commit file is missing: %s\n' "$pinned_ref_file" >&2
	exit 1
fi

models_repository="${MODELS_REPOSITORY_URL:-https://github.com/router-for-me/models.git}"
models_ref="${MODELS_REPOSITORY_REF:-$(tr -d '[:space:]' < "$pinned_ref_file")}"
validate_commit_ref "$models_ref"

catalog_dir="${MODEL_CATALOG_DIR:-$repository_root/internal/registry/models}"
mkdir -p "$catalog_dir"
models_checkout="$(mktemp -d)"
catalog_stage="$(mktemp -d "$catalog_dir/.refresh-model-catalogs.XXXXXX")"
trap 'rm -rf "$models_checkout" "$catalog_stage"' EXIT

git -C "$models_checkout" init --quiet
git -C "$models_checkout" fetch --quiet --depth 1 "$models_repository" "$models_ref"
fetched_commit="$(git -C "$models_checkout" rev-parse 'FETCH_HEAD^{commit}')"
if [[ "$fetched_commit" != "$models_ref" ]]; then
	printf 'fetched models commit %s does not match pinned commit %s\n' "$fetched_commit" "$models_ref" >&2
	exit 1
fi

git -C "$models_checkout" show "FETCH_HEAD:models.json" > "$catalog_stage/models.json"
git -C "$models_checkout" show "FETCH_HEAD:codex_client_models.json" > "$catalog_stage/codex_client_models.json"
(
	cd "$repository_root"
	go run ./cmd/validate_models --file "$catalog_stage/models.json"
	go run ./cmd/validate_codex_models --file "$catalog_stage/codex_client_models.json"
)

mv "$catalog_stage/models.json" "$catalog_dir/models.json"
mv "$catalog_stage/codex_client_models.json" "$catalog_dir/codex_client_models.json"
printf 'Refreshed validated model catalogs from %s.\n' "$models_ref"
