package ghrelease

import "strings"

// nonBinary are name fragments that mark an asset as something other than a
// program: the integrity files published beside it, packages for other
// operating systems, and source archives.
var nonBinary = []string{
	".sha256", ".sha512", ".sha1", ".md5",
	".sig", ".asc", ".txt",
	"checksums", "checksum",
	".deb", ".rpm", ".dmg", ".pkg", ".apk",
	"linux", "darwin", "macos", "android",
	"source", "src",
}

// Excluded reports whether an asset should never be installed, whatever the
// machine. Architecture is deliberately not part of this: the right answer
// there depends on where apptide is running, so it is scored, not excluded.
func Excluded(name string) bool {
	lower := strings.ToLower(name)
	for _, skip := range nonBinary {
		if strings.Contains(lower, skip) {
			return true
		}
	}
	return false
}

// archTokens are the substrings release names use to spell an architecture.
var archTokens = map[string][]string{
	"arm64": {"arm64", "aarch64"},
	"386":   {"i386", "x86_32", "win32", "32bit", "32-bit"},
	"arm":   {"armv7", "armv6", "armhf"},
	"amd64": {"x86_64", "amd64", "x64", "win64", "64bit", "64-bit"},
}

// archOrder is the order archTokens is searched in. amd64 is last because
// "win64" and "64bit" are generic enough to sit beside a more specific token,
// and arm64 comes before arm so "aarch64" is not read as a 32-bit build.
var archOrder = []string{"arm64", "386", "arm", "amd64"}

// ArchOf reports the GOARCH an asset name claims, or "" when it names none.
func ArchOf(name string) string {
	lower := strings.ToLower(name)
	for _, arch := range archOrder {
		for _, tok := range archTokens[arch] {
			if strings.Contains(lower, tok) {
				return arch
			}
		}
	}
	return ""
}

// ArchScore rates how well an asset's architecture fits goarch. The second
// result is false when the asset cannot run on this machine at all.
//
// An asset that names no architecture fits by default: a release with a single
// build is the common case, and refusing it would leave nothing to install.
func ArchScore(name, goarch string) (int, bool) {
	switch arch := ArchOf(name); {
	case arch == goarch:
		return 3, true
	case arch == "":
		return 0, true
	case goarch == "arm64" && arch == "amd64":
		// Windows on ARM runs x64 under emulation. Worse than a native build,
		// but a working last resort rather than a wrong download.
		return 1, true
	default:
		return 0, false
	}
}

// WindowsScore rates how strongly an asset name claims to be for Windows.
func WindowsScore(name string) int {
	lower := strings.ToLower(name)
	for _, win := range []string{"windows", "win64", "win32", "_win_", "-win-", ".win."} {
		if strings.Contains(lower, win) {
			return 5
		}
	}
	return 0
}

// ScoreBinary rates an asset that is expected to be a ready-to-run executable,
// as opposed to an archive to unpack. Returns 0 for assets to skip.
//
// This is what self-update wants: apptide publishes bare .exe files, and an
// archive would have to be unpacked before it could replace the binary.
func ScoreBinary(name, goarch string) int {
	if Excluded(name) {
		return 0
	}
	archScore, ok := ArchScore(name, goarch)
	if !ok {
		return 0
	}

	score := 1 + WindowsScore(name) + archScore
	if strings.HasSuffix(strings.ToLower(name), ".exe") {
		score += 2
	}
	return score
}
