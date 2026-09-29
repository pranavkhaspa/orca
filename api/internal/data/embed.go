package data

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

// baked holds the reference tables and the dated fallback dataset, embedded in
// the binary at build time.
//
// Embedding removes an entire class of deployment failure. The service cannot
// be broken by a missing file, a wrong working directory, a Render build step
// that does not copy static assets, or a read-only filesystem — all of which
// would otherwise leave the service running but unable to answer.
//
//go:embed files/coastal_towns.json files/snapshot.json
var baked embed.FS

// embedPrefix marks a path that should be served from the binary rather than
// from disk. A filesystem path may still be given to override a baked table,
// which is what the snapshot generator uses to write a fresh one.
const embedPrefix = "embed:"

// readData resolves a data path, preferring an explicit filesystem path and
// falling back to the embedded copy when the file is absent.
func readData(path string) ([]byte, error) {
	if strings.HasPrefix(path, embedPrefix) {
		b, err := baked.ReadFile("files/" + strings.TrimPrefix(path, embedPrefix))
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", path, err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err == nil {
		return b, nil
	}
	// Not on disk: try the same name inside the binary before giving up.
	if b2, e2 := baked.ReadFile("files/" + path); e2 == nil {
		return b2, nil
	}
	return nil, err
}

// EmbeddedDataPath is the canonical location of the baked files, used as the
// default by the snapshot generator.
const EmbeddedDataPath = "files/"

// SnapshotsPath is the path the generator writes to.
var SnapshotsPath = EmbeddedDataPath + "snapshot.json"

// CoastalPath is the path the generator reads the reference table from.
var CoastalPath = EmbeddedDataPath + "coastal_towns.json"
