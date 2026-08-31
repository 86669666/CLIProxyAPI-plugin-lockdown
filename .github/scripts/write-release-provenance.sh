#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
	printf 'usage: %s <dist-dir> <source-commit> <models-commit>\n' "$0" >&2
	exit 2
fi

dist_dir="$1"
source_commit="$2"
models_commit="$3"

for entry in "SOURCE_COMMIT:$source_commit" "MODELS_COMMIT:$models_commit"; do
	name="${entry%%:*}"
	commit="${entry#*:}"
	if [[ ! "$commit" =~ ^[0-9a-f]{40}$ ]]; then
		printf '%s must be an immutable lowercase 40-character commit SHA, got %q\n' "$name" "$commit" >&2
		exit 1
	fi
done

printf '%s\n' "$source_commit" > "$dist_dir/SOURCE_COMMIT"
printf '%s\n' "$models_commit" > "$dist_dir/MODELS_COMMIT"

(
	cd "$dist_dir"
	archives=(CLIProxyAPI_*.tar.gz)
	if [[ ! -e "${archives[0]}" ]]; then
		printf 'no release archives found in %s\n' "$dist_dir" >&2
		exit 1
	fi
	sha256sum "${archives[@]}" SOURCE_COMMIT MODELS_COMMIT | sort -k2 > checksums.txt
	sha256sum -c checksums.txt
)
