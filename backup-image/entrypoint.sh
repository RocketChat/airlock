#!/bin/bash

readonly mongodb_uri="${MONGODB_URI?MONGODB_URI is required}"
readonly database="${DATABASE?DATABASE must be passed}"
readonly included_collections="${COLLECTIONS?COLLECTIONS must be passed}"
readonly excluded_collections="${EXCLUDED_COLLECTIONS?EXCLUDED_COLLECTIONS must be passed}"

backup_file="${BACKUP_FILE?BACKUP_FILE must be passed}"

structured_logger() {
	local level key value idx jidx
	level="$1"
	shift 1
	if (($# % 2 == 1)); then
		jo level=error error="invalid key value pairs"
		exit 1
	fi
	local -a args=("level=$level")
	for ((idx = 1; idx <= $#; idx += 2)); do
		jidx=$((idx + 1))
		key="${!idx}"
		value="${!jidx}"
		args+=( "$key=$value" )
	done
	jo "${args[@]}"
}

error() {
	structured_logger error error "$*"
	exit 1
}

info() {
	structured_logger info msg "$*"
}

warn() {
	structured_logger warn msg "$*"
}

debug() {
	structured_logger debug msg "$*"
}

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
	
	local encryption_cmd

	if ! [ -z "$AGE_PRIVATE_KEYS" ]; then
		encryption_cmd="age"
		for key in $AGE_PRIVATE_KEYS; do
			encryption_cmd="$encryption_cmd -r $key"
		done
	fi

	if [ -z "$encryption_cmd" ]; then
		info "encryption disabled, skipping encryption"
		warn "executing \"$cmd > $backup_file\""

		$cmd >"$backup_file" || error "failed to back up database"
	else
		info "encryption enabled, encrypting backup file with age"
		warn "executing \"$cmd | age REDACTED > $backup_file\""

		$cmd | $encryption_cmd >"$backup_file" || error "failed to back up database"
	fi

	[ -f "$backup_file" ] || error "failed to back up db, file not found"

	info "backup finished"
}

restore() {
	error "[restore] function not implemented"
}

s3push() {
	local destination="s3://$BUCKET"
	if [ "$PREFIX" != "" ]; then destination="$destination/$PREFIX"; fi

	__aws() {
		if [ "$NO_VERIFY_SSL" = "true" ]; then
			aws --no-verify-ssl "$@"
		else
			aws "$@"
		fi
	}

	__aws s3 cp "$backup_file" "$destination"
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
