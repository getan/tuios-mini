#!/bin/sh
# Run golder's live client contract against a freshly built tuios-mini.
#
# The test lives in the golder repository because it uses golder's own client.
# Set GOLDER_DIR when the two repositories are not siblings.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
golder=${GOLDER_DIR:-$here/../golder}
if [ ! -d "$golder" ]; then
	echo "check-golder: GOLDER_DIR=$golder is not a directory" >&2
	exit 2
fi

# The daemon derives link and herdr socket names from its own path, so keep
# the whole path well under the Unix limit (about 104 bytes).
tmp=$(mktemp -d /tmp/tmini-check.XXXXXX)
pid=
cleanup() {
	if [ -n "$pid" ]; then
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	fi
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

cp "$here/mini/testdata/integrated-sh" "$tmp/integrated-sh"
chmod +x "$tmp/integrated-sh"
(cd "$here" && go build -o "$tmp/tuios-mini" ./cmd/tuios-mini)

"$tmp/tuios-mini" --socket "$tmp/d.sock" --session work \
	--shell "$tmp/integrated-sh" --cwd "$tmp" \
	>"$tmp/ready.json" 2>"$tmp/mini.log" &
pid=$!

i=0
while [ ! -S "$tmp/d.sock" ] && [ "$i" -lt 200 ]; do
	i=$((i + 1))
	sleep 0.05
done
if [ ! -S "$tmp/d.sock" ]; then
	cat "$tmp/mini.log" >&2
	exit 1
fi

(cd "$golder" && GOLDER_TUIOS_LIVE_SOCKET="$tmp/d.sock" GOLDER_TUIOS_LIVE_SESSION=work \
	go test ./internal/tuiosctl -run TestLiveMiniDaemonContract -count=1 -v)
