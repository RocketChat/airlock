#!/bin/sh

readonly mongodb_uri="${MONGODB_URI?MONGODB_URI is required}"
readonly database="${DATABASE?DATABASE must be passed}"

backup_file="${BACKUP_FILE}"

structured_logger() {
	level=$1
	shift 1
	if [ $(( $# % 2 )) -eq 1 ]; then
		jo level=error msg="invalid key value pairs"
		exit 1
	fi
	while [ $# -ge 2 ]; do
		key=$1
		value=$2
		shift 2
		set -- "$@" "${key}=${value}"
	done
	set -- "level=$level" "$@"
	jo "$@"
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


aws() {
	if [ "$NO_VERIFY_SSL" = "true" ]; then
		command aws --no-verify-ssl "$@"
	else
		command aws "$@"
	fi
}

dump() {
	info "starting in dump mode"
	if [ -z "$backup_file" ]; then
		error "BACKUP_FILE is required"
	fi

	local included_collections="${COLLECTIONS}"
	local excluded_collections="${EXCLUDED_COLLECTIONS}"

	local excluded_arg excluded_col

	for excluded_col in $(echo $excluded_collections | tr ',' ' '); do
		excluded_arg="${excluded_arg} --excludeCollection=$excluded_col"
	done

	local included_arg included_col

	for included_col in $(echo $included_collections | tr ',' ' '); do
		included_arg="${included_arg} --collection=$included_col"
	done

	local cmd="mongodump --uri=$mongodb_uri -d $database --archive --gzip"
	
	if [ -n "$included_collections" ]; then
		cmd="$cmd --collection=$included_collections"
	fi

	if [ -n "$excluded_collections" ]; then
		cmd="$cmd --excludeCollection=$excluded_collections"
	fi
	
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

trim_starting_slash() {
	echo "$1" | sed "s/^\///"
}

restore() {
	set -x
	info "starting in restore mode"
	
	local bucket="$BUCKET"
	if [ -z "$bucket" ]; then
		error "BUCKET is required"
	fi
	
	local s3_path="${S3_PATH}"
	if [ -z "$s3_path" ]; then
		error "S3_PATH is required"
	fi
	
	local full_path="s3://$(trim_trailing_slash "$bucket")/$(trim_starting_slash "$s3_path")"
	
	info "downloading backup from $full_path and restoring"
	aws s3 cp "$full_path" - | \
		mongorestore --uri="$mongodb_uri" --drop --archive || error "failed to restore database"
	
	info "restore finished"
}

trim_trailing_slash() {
	echo "$1" | sed "s/\/$//"
}

s3push() {
	if [ -z "$BUCKET" ]; then
		error "BUCKET is required"
	fi
	
	local destination="s3://$(trim_trailing_slash "$BUCKET")"
	if [ -n "$PREFIX" ]; then destination="${destination}/$(trim_trailing_slash "$PREFIX")"; fi

	aws s3 cp "$backup_file" "$destination/$(basename "$backup_file")"
}

main() {
	case "$1" in
	"backup")
		dump
		s3push
		;;
	"restore")
		restore
		;;
	esac
}

main "$@"
