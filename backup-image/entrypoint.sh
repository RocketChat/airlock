#!/bin/sh

readonly mongodb_uri="${MONGODB_URI?MONGODB_URI is required}"
readonly database="${DATABASE?DATABASE must be passed}"
readonly included_collections="${COLLECTIONS?COLLECTIONS must be passed}"
readonly excluded_collections="${EXCLUDED_COLLECTIONS?EXCLUDED_COLLECTIONS must be passed}"

readonly backup_file="${BACKUP_FILE?BACKUP_FILE must be passed}"

readonly red="\e[31m"
readonly blue="\e[34m"
readonly yellow="\e[33m"
readonly reset="\e[0m"

error() {
	echo "${red}[Error]${reset} $*" >&2
	exit 1
}

info() {
	echo "${blue}[Info]${reset} $*"
}

warn() {
	echo "${yellow}[Warn]${reset} $*"
}

debug() {
	echo "[Debug] $*"
}

# dump() {
# 	info "starting in dump mode"

# 	local excluded_arg excluded_col

# 	for excluded_col in $(echo $excluded_collections | tr ',' ' '); do
# 		excluded_arg="${excluded_arg}${database}.${excluded_col},"
# 	done

# 	excluded_arg="$(echo "$excluded_arg" | sed 's/,$//')"

# 	debug "--nsExclude=$excluded_arg"

# 	local included_arg included_col

# 	for included_col in $(echo $included_collections | tr ',' ' '); do
# 		included_arg="${included_arg}${database}.${included_col},"
# 	done

# 	included_arg="$(echo "$included_arg" | sed 's/,$//')"

# 	debug "--nsInclude=$included_arg"

# 	local cmd="mongodump --uri=$mongodb_uri --nsInclude=$included_arg --nsExclude=$excluded_arg --archive --gzip"

# 	warn "executing \"$cmd > $backup_file\""

# 	$cmd >"$backup_file" || error "failed to back up database"

# 	info "backup finished"
# }

dump() {
	info "starting in dump mode"

	local excluded_arg excluded_col

	for excluded_col in $(echo $excluded_collections | tr ',' ' '); do
		excluded_arg="${excluded_arg} --excludeCollection=$excluded_col"
	done

	local included_arg included_col

	for included_col in $(echo $included_collections | tr ',' ' '); do
		included_arg="${included_arg} --collection=$included_col"
	done

	local cmd="mongodump --uri=$mongodb_uri $included_arg $excluded_arg -d $database --archive --gzip"

	warn "executing \"$cmd > $backup_file\""

	$cmd >"$backup_file" || error "failed to back up database"

	[ -f "$backup_file" ] || error "failed to back up db, file not found"

	info "backup finished"
}

restore() {
	error "[restore] function not implemented"
}

s3push() {
	info "starting to split and push dump to s3"

	local split_size="${SPLIT_SIZE:-1024}"
	local split_prefix="${backup_file}.part"

	info "splitting backup file into ${split_size}MB parts"

	split -b "${split_size}m" -d -a 3 "$backup_file" "$split_prefix" || error "failed to split backup archive"

	info "backup file split successfully"

	# List the created parts for verification
	# shellcheck disable=SC2046
	local parts=$(ls "${split_prefix}"* 2>/dev/null | wc -l)
	info "created $parts backup parts"

	# shellcheck disable=SC2046
	local manifest="$(generate_manifest "$backup_file" $(ls "${split_prefix}"*))"

	debug "pretty manifest: $(echo "$manifest" | jq)"

	echo "$manifest" >manifest.json

	local destination="s3://$BUCKET"
	if [ "$PREFIX" != "" ]; then destination="$destination/$PREFIX"; fi

	__aws() {
		if [ "$NO_VERIFY_SSL" = "true" ]; then
			aws --no-verify-ssl "$@"
		else
			aws "$@"
		fi
	}

	__aws s3 cp manifest.json "$destination"

	# TODO: push this manifest first

	# TODO: Upload each part to S3
	for part in "${split_prefix}"*; do
		debug "part: $part"
		__aws s3 cp "$part" "$destination"
	done
}

hash() {
	sha256sum "$1" | awk '{print $1}'
}

hash_json() {
	printf '{"hash":{"sha256":"%s"},"filename":"%s"}' "$(hash "$1")" "$(basename "$1")"
}

generate_manifest() {
	local manifest='{"version":1,"parts":' part

	local full_file="$1"

	shift

	local parts_json=

	for part in "$@"; do
		parts_json="${parts_json}$(hash_json "$part"),"
	done

	parts_json="$(echo "$parts_json" | sed 's/,$//')"

	manifest="${manifest}[${parts_json}],$(hash_json "$full_file" | sed -E 's/^\{(.+)\}$/\1/')}"

	echo "$manifest"
}

main() {
	case "$1" in
	"backup")
		dump
		s3push
		;;
	"restore")
		error "not implemented"
		;;
	esac
}

main "$@"
