#!/bin/sh

main() (
	set -eu

	fail() {
		printf 'builder: %s\n' "$*" >&2
		exit 1
	}

	download() {
		curl -fsSL --proto '=https' --proto-redir '=https' --retry 3 --connect-timeout 30 "$@"
	}

	as_installer() {
		if [ "$use_sudo" = true ]; then
			sudo -- "$@"
		else
			"$@"
		fi
	}

	cleanup() {
		if [ -n "$staging" ]; then
			as_installer rm -rf -- "$staging"
			staging=''
		fi

		if [ -n "$temporary" ]; then
			rm -rf -- "$temporary"
			temporary=''
		fi
	}

	install_dir=/usr/local/bin

	while [ "$#" -gt 0 ]; do
		case "$1" in
			--dir)
				[ "$#" -ge 2 ] && [ -n "$2" ] || fail '--dir requires a directory'
				install_dir=$2
				shift
				;;
			-h|--help)
				printf 'Usage: sh install.sh [--dir DIRECTORY]\n'
				printf 'Installs the latest builder release to /usr/local/bin using root or sudo.\n'
				printf 'Use --dir to install to a user-writable directory instead.\n'
				exit 0
				;;
			*)
				fail "unknown option: $1"
				;;
		esac
		shift
	done

	case "$(uname -s)" in
		Linux) platform=linux ;;
		Darwin) platform=darwin ;;
		*) fail 'supported systems are Linux and macOS; use install.ps1 on Windows' ;;
	esac

	case "$(uname -m)" in
		x86_64|amd64) architecture=amd64 ;;
		*) fail 'prebuilt releases currently support amd64 only; build from source for this architecture' ;;
	esac

	for dependency in curl mktemp install mv; do
		command -v "$dependency" >/dev/null 2>&1 || fail "required command not found: $dependency"
	done

	if command -v sha256sum >/dev/null 2>&1; then
		checksum_tool=sha256sum
	elif command -v shasum >/dev/null 2>&1; then
		checksum_tool=shasum
	else
		fail 'sha256sum or shasum is required'
	fi

	case "$install_dir" in
		/*) ;;
		*) install_dir=$PWD/$install_dir ;;
	esac

	[ "$install_dir" = / ] || install_dir=${install_dir%/}
	destination=$install_dir/builder
	[ ! -d "$destination" ] || fail "$destination is a directory"

	user_id=$(id -u)
	use_sudo=false
	if [ "$install_dir" = /usr/local/bin ] && [ "$user_id" != 0 ]; then
		command -v sudo >/dev/null 2>&1 || fail 'installing to /usr/local/bin requires root or sudo'
		use_sudo=true
	fi

	temporary=''
	staging=''
	trap cleanup 0
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM
	temporary=$(mktemp -d)

	repository=https://github.com/coalaura/builder
	release_url=$(download -o /dev/null -w '%{url_effective}' "$repository/releases/latest")
	case "$release_url" in
		"$repository/releases/tag/"*) tag=${release_url##*/} ;;
		*) fail 'could not resolve the latest stable release' ;;
	esac

	case "$tag" in
		''|*[!A-Za-z0-9.+-]*) fail 'invalid release tag' ;;
	esac

	asset=builder-$platform-$architecture
	release_url=$repository/releases/download/$tag
	printf 'Downloading builder %s...\n' "$tag"
	download -o "$temporary/$asset" "$release_url/$asset"
	download -o "$temporary/SHA256SUMS" "$release_url/SHA256SUMS"

	expected_checksum=$(awk -v asset="$asset" '$2 == asset || $2 == "*" asset { print $1 }' "$temporary/SHA256SUMS")
	[ "${#expected_checksum}" -eq 64 ] || fail "missing or invalid checksum for $asset"

	case "$expected_checksum" in
		*[!0-9a-fA-F]*) fail "invalid checksum for $asset" ;;
	esac

	if [ "$checksum_tool" = sha256sum ]; then
		actual_checksum=$(sha256sum "$temporary/$asset")
	else
		actual_checksum=$(shasum -a 256 "$temporary/$asset")
	fi

	actual_checksum=${actual_checksum%% *}
	expected_checksum=$(printf '%s' "$expected_checksum" | tr 'A-F' 'a-f')
	[ "$actual_checksum" = "$expected_checksum" ] || fail "checksum verification failed for $asset"

	as_installer install -d -m 755 "$install_dir"
	# Stage on the destination filesystem so replacement is atomic across filesystems.
	staging=$(as_installer mktemp -d "$install_dir/.builder-install.XXXXXXXX")
	if [ "$use_sudo" = true ] || [ "$user_id" = 0 ]; then
		as_installer install -m 755 -o 0 -g 0 "$temporary/$asset" "$staging/builder"
	else
		install -m 755 "$temporary/$asset" "$staging/builder"
	fi

	case "$platform" in
		linux) as_installer mv -fT -- "$staging/builder" "$destination" ;;
		darwin) as_installer mv -fh "$staging/builder" "$destination" ;;
	esac

	cleanup
	printf 'Installed builder %s to %s\n' "$tag" "$destination"

	case ":${PATH:-}:" in
		*:"$install_dir":*) ;;
		*)
			quoted_dir=$(printf '%s' "$install_dir" | sed "s/'/'\\\\''/g")
			printf '\nAdd this line to your shell profile, then open a new terminal:\n'
			printf "export PATH='%s':\"\$PATH\"\n" "$quoted_dir"
			;;
	esac

	# A piped installer owns stdin; both the confirmation and setup need the terminal.
	if [ ! -t 0 ]; then
		if ( : </dev/tty ) 2>/dev/null; then
			exec </dev/tty
		else
			printf 'Skipping setup (no interactive terminal). Run builder setup later.\n'
			exit 0
		fi
	fi

	printf '\nRun builder setup now? [y/N] '
	answer=''
	read -r answer || answer=''

	case "$answer" in
		[yY]|[yY][eE][sS]) "$destination" setup ;;
	esac
)

main "$@"
