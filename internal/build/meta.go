package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type BuildMeta struct {
	Status         string     `json:"status"`
	Started        *time.Time `json:"started,omitempty"`
	Finished       *time.Time `json:"finished,omitempty"`
	Duration       float64    `json:"duration_seconds"`
	DiskBytes      int64      `json:"disk_bytes"`
	InstalledBytes int64      `json:"installed_bytes"`
	Hash           string     `json:"hash"`
	Error          string     `json:"error,omitempty"`
	SourceState    string     `json:"source_state,omitempty"`

	SourceDescribe string `json:"source_describe,omitempty"`
}

const metaFile = "build.json"

func MetaPath(buildDir string) string {
	return filepath.Join(buildDir, metaFile)
}

func WriteMeta(buildDir string, meta *BuildMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(MetaPath(buildDir), data, 0644)
}

func ReadMeta(buildDir string) *BuildMeta {
	data, err := os.ReadFile(MetaPath(buildDir))
	if err != nil {
		return nil
	}
	var meta BuildMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &meta
}

func initBuildMeta(buildDir, hash string, started time.Time) *BuildMeta {
	meta := &BuildMeta{
		Status:  "building",
		Started: &started,
		Hash:    hash,
	}
	if prev := ReadMeta(buildDir); prev != nil {
		meta.SourceState = prev.SourceState
		meta.SourceDescribe = prev.SourceDescribe
	}
	return meta
}

func DirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}
