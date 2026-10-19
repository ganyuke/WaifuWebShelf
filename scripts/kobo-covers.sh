#!/usr/bin/env bash
# Download full-size Kobo covers.
# Usage: scripts/kobo-covers.sh [-d DIR] LINK...
#
# LINK is a store page (www.kobo.com/.../ebook/NAME-vol-14), which gets every
# volume of that series, or a cover link or bare image ID, which gets that one.
#
# A volume page lists the covers of all volumes in its series, mixed in with
# recommendations, so one fetch is enough. Volumes are picked by name: the
# page's own name with its last number swapped for any number.
#
# Store pages answer plain curl with a Cloudflare challenge. Sending the
# Sec-Fetch headers a browser sends for a page load is enough to get the page.
#
# The CDN path is book-images/ID/WIDTH/HEIGHT/QUALITY/GREYSCALE/NAME.EXT,
# where WIDTH sets the output width and upscales past the source, and 0/0
# returns the stored size. Quality 100 keeps JPEG re-encoding loss down.
set -euo pipefail

usage="Usage: scripts/kobo-covers.sh [-d DIR] LINK..."
dir=.
while getopts d:h opt; do
  case "$opt" in
    d) dir=$OPTARG ;;
    *)
      echo "$usage" >&2
      exit 2
      ;;
  esac
done
shift $((OPTIND - 1))
if [[ $# -eq 0 ]]; then
  echo "$usage" >&2
  exit 2
fi
mkdir -p "$dir"

ua="WaifuWebShelf-kobo-covers"
uuid='[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}'
failed=0

# save ID NAME downloads one cover at its stored size to DIR/NAME.jpg.
save() {
  local id=$1 name=$2 out
  # Keep the name to plain file-safe characters.
  name=$(tr -c 'A-Za-z0-9._-' '-' <<<"$name" | sed 's/-*$//')
  [[ -n $name && $name != . && $name != .. ]] || name=$id
  out=$dir/$name.jpg
  if [[ -e $out ]]; then
    echo "Skipped $out: already there"
    return
  fi
  if curl -fsS --retry 2 -A "$ua" -o "$out.part" \
    "https://cdn.kobo.com/book-images/$id/0/0/100/False/$name.jpg"; then
    mv "$out.part" "$out"
    echo "Saved $out"
  else
    if [[ -e $out.part ]]; then rm -- "$out.part"; fi
    echo "Could not download $name" >&2
    failed=1
  fi
}

# series PAGE_URL downloads every volume of the series listed on that page.
series() {
  local url=$1 slug page prefix suffix name id rest found=0
  slug=${url%%[?#]*}
  slug=${slug%/}
  slug=${slug##*/}
  if ! page=$(curl -fsS --compressed --retry 2 -A "$ua" \
    -H 'Sec-Fetch-Dest: document' -H 'Sec-Fetch-Mode: navigate' \
    -H 'Sec-Fetch-Site: none' -H 'Sec-Fetch-User: ?1' "$url"); then
    echo "Could not open $url" >&2
    failed=1
    return
  fi
  prefix=$slug suffix=
  if [[ $slug =~ ^(.*[^0-9])[0-9]+([^0-9]*)$ ]]; then
    prefix=${BASH_REMATCH[1]} suffix=${BASH_REMATCH[2]}
  fi
  while read -r name id; do
    [[ $name == "$prefix"*"$suffix" ]] || continue
    rest=${name#"$prefix"}
    rest=${rest%"$suffix"}
    [[ $name == "$slug" || $rest =~ ^[0-9]+$ ]] || continue
    found=$((found + 1))
    save "$id" "$name"
  done < <(grep -oE "book-images/$uuid/[^\"' \\]*" <<<"$page" |
    sed -E "s#^book-images/($uuid)/.*/([^/]*)\.[A-Za-z]+\$#\2 \1#" |
    sort -u -k1,1 | sort -V)
  if [[ $found -eq 0 ]]; then
    echo "No covers found on $url" >&2
    failed=1
  fi
}

for arg in "$@"; do
  if [[ $arg =~ ^https?://(www\.)?kobo\.com/.+/(ebook|audiobook)/[^/?#]+ ]]; then
    series "$arg"
  elif [[ $arg =~ book-images/($uuid)(/[^?#]*)? ]]; then
    name=${BASH_REMATCH[2]##*/}
    save "${BASH_REMATCH[1]}" "${name%.*}"
  elif [[ $arg =~ ^($uuid)$ ]]; then
    save "${BASH_REMATCH[1]}" ""
  else
    echo "Skipped $arg: not a Kobo store page or cover link" >&2
    failed=1
  fi
done
exit "$failed"
