package goenv

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type macOSSDK struct {
	path    string
	version []int
}

func discoverMacOSSDK(cwd, home, pathValue, sdkRoot, hostOS string) string {
	if sdkRoot != "" {
		sdkRoot = absolutePath(cwd, sdkRoot)

		if validMacOSSDK(sdkRoot) {
			return sdkRoot
		}
	}

	candidates := make([]string, 0, 9+2*len(filepath.SplitList(pathValue)))
	seen := make(map[string]bool, cap(candidates))

	candidates = appendSDKCandidate(candidates, seen, filepath.Join(cwd, "xmac-sdk", "SDKs"), hostOS)
	candidates = appendSDKCandidate(candidates, seen, filepath.Join(cwd, "osxcross", "target", "SDK"), hostOS)
	candidates = appendSDKCandidate(candidates, seen, filepath.Join(cwd, "target", "SDK"), hostOS)

	for _, entry := range filepath.SplitList(pathValue) {
		entry = absolutePath(cwd, entry)

		if !pathBaseEqual(filepath.Base(entry), "bin", hostOS) {
			continue
		}

		resolved, err := filepath.EvalSymlinks(entry)
		if err == nil {
			entry = resolved
		}

		prefix := filepath.Dir(entry)

		candidates = appendSDKCandidate(candidates, seen, filepath.Join(prefix, "SDK"), hostOS)
		candidates = appendSDKCandidate(candidates, seen, filepath.Join(prefix, "SDKs"), hostOS)
	}

	if home != "" {
		candidates = appendSDKCandidate(candidates, seen, filepath.Join(home, "xmac-sdk", "SDKs"), hostOS)
		candidates = appendSDKCandidate(candidates, seen, filepath.Join(home, "osxcross", "target", "SDK"), hostOS)
	}

	if hostOS == "linux" {
		candidates = appendSDKCandidate(candidates, seen, "/usr/local/osxcross/SDK", hostOS)
		candidates = appendSDKCandidate(candidates, seen, "/usr/local/osxcross/target/SDK", hostOS)
		candidates = appendSDKCandidate(candidates, seen, "/opt/osxcross/SDK", hostOS)
		candidates = appendSDKCandidate(candidates, seen, "/opt/osxcross/target/SDK", hostOS)
	}

	for _, candidate := range candidates {
		sdk := findMacOSSDK(candidate)

		if sdk != "" {
			return sdk
		}
	}

	return ""
}

func appendSDKCandidate(candidates []string, seen map[string]bool, candidate, hostOS string) []string {
	candidate = absolutePath("", candidate)

	resolved, err := filepath.EvalSymlinks(candidate)
	if err == nil {
		candidate = resolved
	}

	key := candidate

	if hostOS == "windows" {
		key = strings.ToLower(key)
	}

	if seen[key] {
		return candidates
	}

	seen[key] = true

	return append(candidates, candidate)
}

func findMacOSSDK(directory string) string {
	alias := filepath.Join(directory, "MacOSX.sdk")

	if validMacOSSDK(alias) {
		return absolutePath("", alias)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}

	sdks := make([]macOSSDK, 0, len(entries))

	for _, entry := range entries {
		version, valid := macOSSDKVersion(entry.Name())
		if !valid {
			continue
		}

		path := filepath.Join(directory, entry.Name())
		if !validMacOSSDK(path) {
			continue
		}

		sdks = append(sdks, macOSSDK{path: path, version: version})
	}

	sort.Slice(sdks, func(left, right int) bool {
		comparison := compareSDKVersions(sdks[left].version, sdks[right].version)
		if comparison == 0 {
			return sdks[left].path > sdks[right].path
		}

		return comparison > 0
	})

	if len(sdks) == 0 {
		return ""
	}

	return absolutePath("", sdks[0].path)
}

func macOSSDKVersion(name string) ([]int, bool) {
	if !strings.HasPrefix(name, "MacOSX") || !strings.HasSuffix(name, ".sdk") {
		return nil, false
	}

	value := strings.TrimSuffix(strings.TrimPrefix(name, "MacOSX"), ".sdk")
	if value == "" {
		return nil, false
	}

	parts := make([]int, 0, strings.Count(value, ".")+1)

	for part := range strings.SplitSeq(value, ".") {
		if part == "" {
			return nil, false
		}

		for _, character := range part {
			if character < '0' || character > '9' {
				return nil, false
			}
		}

		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}

		parts = append(parts, number)
	}

	return parts, true
}

func compareSDKVersions(left, right []int) int {
	length := max(len(left), len(right))

	for index := range length {
		leftPart := 0

		if index < len(left) {
			leftPart = left[index]
		}

		rightPart := 0

		if index < len(right) {
			rightPart = right[index]
		}

		if leftPart < rightPart {
			return -1
		}

		if leftPart > rightPart {
			return 1
		}
	}

	return 0
}

func validMacOSSDK(path string) bool {
	directories := [...]string{
		filepath.Join(path, "usr", "include"),
		filepath.Join(path, "System", "Library", "Frameworks"),
	}

	for _, directory := range directories {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() {
			return false
		}
	}

	info, err := os.Stat(filepath.Join(path, "usr", "lib", "libSystem.tbd"))
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func absolutePath(base, path string) string {
	if base != "" && !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}

	return absolute
}

func pathBaseEqual(left, right, hostOS string) bool {
	if hostOS == "windows" {
		return strings.EqualFold(left, right)
	}

	return left == right
}
