package deploy

import (
	"io"
	"os"
	"path/filepath"

	archive "github.com/moby/go-archive"
	"github.com/moby/patternmatcher/ignorefile"
)

const (
	Dockerfile   = "Dockerfile"
	Dockerignore = ".dockerignore"
)

func CreateBuildContext(contextDir string, dockerfile *string) (io.ReadCloser, error) {
	df := Dockerfile
	if dockerfile != nil {
		df = *dockerfile
	}

	excludes, err := readDockerignore(contextDir)
	if err != nil {
		return nil, err
	}

	excludes = trimBuildFilesFromExcludes(excludes, df)

	tarOptions := &archive.TarOptions{
		ExcludePatterns: excludes,
	}

	return archive.TarWithOptions(contextDir, tarOptions)
}

func readDockerignore(contextDir string) ([]string, error) {
	dockerignorePath := filepath.Join(contextDir, Dockerignore)

	f, err := os.Open(dockerignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	return ignorefile.ReadAll(f)
}

func trimBuildFilesFromExcludes(excludes []string, dockerfile string) []string {
	if len(excludes) == 0 {
		return excludes
	}

	keepFiles := map[string]bool{
		dockerfile:   true,
		Dockerignore: true,
	}

	var filtered []string
	for _, pattern := range excludes {
		if !keepFiles[pattern] {
			filtered = append(filtered, pattern)
		}
	}

	return filtered
}
