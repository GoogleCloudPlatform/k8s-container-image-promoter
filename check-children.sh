#!/bin/bash
TMP_DIR=$(mktemp -d)
PROD_REPO="us.gcr.io/k8s-artifacts-prod"
SNAPSHOT_FILE="$TMP_DIR/snap.txt"

verify_child() {
	local parent="$1"
	local child="$2"
	local existing
	existing=$(docker inspect "$child")
	if [[ "$existing" == "[]" ]]; then
		echo -e "ERROR: parent \"$parent\" was found to be from a different location than it's child."
		exit 1
	fi
}

verify_children() {
	local long_img_name="$1"
	local complete_img_name="$2"
	local manifest_output

	# Obtain the manifest for the image.
	manifest_output=$(docker manifest inspect "$long_img_name")

	# Parse the manifest, capturing all children.
	local children=()
	within_manifests="false"
	while read -r line; do
		if [[ "$within_manifests" == "true" ]]; then
			# Finish parsing when the manifests array is closed.
			if [[ "$line" == "]" ]]; then
				break
			elif [[ $line =~ ^"\"digest\":"* ]]; then
				child_sha256=${line:11:71}
				expected_child_name="${long_img_name}@${child_sha256}"
				children+=("$expected_child_name")
			fi
		elif [[ $line == "\"manifests\": [" ]]; then
			within_manifests="true"
		fi
	done < <(echo "$manifest_output")

	# Verify all children found.
	for child in "${children[@]}"; do
		# Will fail for invalid children.
		verify_child "$child"
	done
}

generate_snapshot() {
	make build
	./cip run --snapshot "$PROD_REPO"
	# ./cip run --snapshot "$PROD_REPO" > "$SNAPSHOT_FILE"
}

parse_snapshot() {
	img_name=""
	while read -ru 10 line; do
		if [[ $line =~ ^"- name:"* ]]; then
			img_name="${line:8}"
		elif [[ $line =~ ^"\"sha256:"* ]]; then
			sha256=${line:1:71}
			# example: us.gcr.io/k8s-artifacts-prod/some-img"
			long_img_name="${PROD_REPO}/${img_name}"
			# example: us.gcr.io/k8s-artifacts-prod/some-img@sha2564123847619238746123"
			complete_img_name="${long_img_name}@${sha256}"
			# Ensure all children have the same long_img_name
			verify_children "$long_img_name" "$complete_img_name"
		fi
		
	done 10<"$SNAPSHOT_FILE"
}

main() {
	generate_snapshot
	parse_snapshot
}

main
