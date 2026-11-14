#!/usr/bin/env bash
# Writes the Homebrew formula (Formula/linos.rb) and the Scoop manifest (bucket/linos.json) for a release,
# from the checksums of its binaries. Run by release.yml: packaging.sh v0.1.0 out/SHA256SUMS
# Both live in this repository, so it serves as Homebrew tap and Scoop bucket:
#   brew tap gwenaf/linos https://github.com/gwenaf/Linos && brew install linos
#   scoop bucket add linos https://github.com/gwenaf/Linos && scoop install linos
set -euo pipefail

tag=$1 sums=$2
version=${tag#v}
base=https://github.com/gwenaf/Linos/releases/download/$tag
sum() {
	local s
	s=$(awk -v f="$1" '$2 == f || $2 == "*" f { print $1 }' "$sums")
	[ -n "$s" ] || { echo "no checksum for $1 in $sums" >&2; exit 1; }
	echo "$s"
}

# Computed first: a failure inside a heredoc would not stop the script.
mac_arm=$(sum linos-darwin-arm64)
mac_intel=$(sum linos-darwin-amd64)
linux_arm=$(sum linos-linux-arm64)
linux_intel=$(sum linos-linux-amd64)
windows=$(sum linos.exe)

mkdir -p Formula bucket
cat >Formula/linos.rb <<EOF
# Written by .github/packaging.sh on each release.
class Linos < Formula
  desc "Portable, offline blind test: the TV plays, phones buzz"
  homepage "https://github.com/gwenaf/Linos"
  version "$version"
  license "AGPL-3.0-only"

  on_macos do
    on_arm do
      url "$base/linos-darwin-arm64"
      sha256 "$mac_arm"
    end
    on_intel do
      url "$base/linos-darwin-amd64"
      sha256 "$mac_intel"
    end
  end
  on_linux do
    on_arm do
      url "$base/linos-linux-arm64"
      sha256 "$linux_arm"
    end
    on_intel do
      url "$base/linos-linux-amd64"
      sha256 "$linux_intel"
    end
  end

  def install
    libexec.install Dir["linos-*"].first => "linos"
    chmod 0755, libexec/"linos"
    # packs/ and linos.log go to ~/Linos: the Homebrew prefix is replaced on each upgrade.
    (bin/"linos").write_env_script libexec/"linos", LINOS_HOME: "\${LINOS_HOME:-\$HOME/Linos}"
  end

  test do
    assert_predicate libexec/"linos", :executable?
  end
end
EOF

cat >bucket/linos.json <<EOF
{
  "version": "$version",
  "description": "Portable, offline blind test: the TV plays, phones buzz",
  "homepage": "https://github.com/gwenaf/Linos",
  "license": "AGPL-3.0-only",
  "architecture": {
    "64bit": {
      "url": "$base/linos.exe",
      "hash": "$windows"
    }
  },
  "bin": "linos.exe",
  "persist": "packs"
}
EOF
