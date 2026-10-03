package apkindex

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Entry struct {
	Name          string
	Version       string
	Description   string
	URL           string
	License       string
	Arch          string
	Size          int64
	InstalledSize int64
	Origin        string
	Maintainer    string
	BuildTime     int64
	Commit        string

	Checksum     []byte
	ChecksumText string

	Deps      []string
	Provides  []string
	Replaces  []string
	InstallIf []string
}

func ParseIndex(r io.Reader) ([]Entry, error) {
	var entries []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)

	var (
		cur     Entry
		curHas  bool
		lineNum int
		blockLn int
	)

	flush := func() error {
		if !curHas {
			return nil
		}
		if cur.Name == "" {
			return fmt.Errorf("apkindex: line %d: block has no P: (package name)", blockLn)
		}
		if cur.ChecksumText != "" {
			raw, err := decodeChecksum(cur.ChecksumText)
			if err != nil {
				return fmt.Errorf("apkindex: line %d: %s: %w", blockLn, cur.Name, err)
			}
			cur.Checksum = raw
		}
		entries = append(entries, cur)
		cur = Entry{}
		curHas = false
		return nil
	}

	for sc.Scan() {
		lineNum++
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if len(line) < 2 || line[1] != ':' {
			continue
		}
		if !curHas {
			blockLn = lineNum
			curHas = true
		}
		key := line[0]
		val := line[2:]
		if err := setField(&cur, key, val, lineNum); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("apkindex: scan: %w", err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return entries, nil
}

func ParseIndexFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseIndex(f)
}

func setField(cur *Entry, key byte, val string, lineNum int) error {
	switch key {
	case 'P':
		cur.Name = val
	case 'V':
		cur.Version = val
	case 'T':
		cur.Description = val
	case 'U':
		cur.URL = val
	case 'L':
		cur.License = val
	case 'A':
		cur.Arch = val
	case 'S':
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("apkindex: line %d: S: %w", lineNum, err)
		}
		cur.Size = n
	case 'I':
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("apkindex: line %d: I: %w", lineNum, err)
		}
		cur.InstalledSize = n
	case 'o':
		cur.Origin = val
	case 'm':
		cur.Maintainer = val
	case 't':
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("apkindex: line %d: t: %w", lineNum, err)
		}
		cur.BuildTime = n
	case 'c':
		cur.Commit = val
	case 'C':
		cur.ChecksumText = val
	case 'D':
		cur.Deps = splitTokens(val)
	case 'p':
		cur.Provides = splitTokens(val)
	case 'r':
		cur.Replaces = splitTokens(val)
	case 'i':
		cur.InstallIf = splitTokens(val)
	}
	return nil
}

func splitTokens(s string) []string {
	fs := strings.Fields(s)
	if len(fs) == 0 {
		return nil
	}
	return fs
}

func decodeChecksum(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "Q1") {
		return nil, fmt.Errorf("apk_checksum: expected Q1 (sha1) prefix, got %q", s)
	}
	raw, err := base64.StdEncoding.DecodeString(s[2:])
	if err != nil {
		return nil, fmt.Errorf("apk_checksum: base64 decode: %w", err)
	}
	if len(raw) != sha1.Size {
		return nil, fmt.Errorf("apk_checksum: wanted %d sha1 bytes, got %d",
			sha1.Size, len(raw))
	}
	return raw, nil
}
