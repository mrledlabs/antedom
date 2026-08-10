package antedom

import (
	"hash/fnv"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
)

// fingerprint hashes every file's path, size, and mtime under roots,
// each a directory or a single file. LiveReload polls it to detect
// site edits; serve's artifact cache will compare it to decide
// whether cached build outputs are stale. Walk errors are skipped:
// editors rename and remove files mid-save, and the next poll sees
// the settled state.
func fingerprint(roots ...string) uint64 {
	h := fnv.New64a()
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			io.WriteString(h, p)
			h.Write([]byte{0})
			io.WriteString(h, info.ModTime().String())
			h.Write([]byte{0})
			io.WriteString(h, strconv.FormatInt(info.Size(), 10))
			h.Write([]byte{0})
			return nil
		})
	}
	return h.Sum64()
}
