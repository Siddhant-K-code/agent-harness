package workspace

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const MaxSnapshotBytes int64 = 64 << 20
const MaxSnapshotFiles = 10000

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Snapshot struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Files  int    `json:"files"`
}

func safeLink(name, target string) bool {
	if target == "" || path.IsAbs(target) || strings.Contains(target, "\\") {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}

// SnapshotTo captures a quiescent workspace, including untracked files. Agent
// processes must be stopped first. OpenRoot prevents reads outside the tree.
func (w Workspace) SnapshotTo(ctx context.Context, directory string) (Snapshot, error) {
	var result Snapshot
	root, err := os.OpenRoot(w.Path)
	if err != nil {
		return result, err
	}
	defer root.Close()
	if err := os.MkdirAll(directory, 0700); err != nil {
		return result, err
	}
	f, err := os.CreateTemp(directory, ".snapshot-")
	if err != nil {
		return result, err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	defer f.Close()
	hash := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, hash))
	var payload int64
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if name == ".git" || strings.HasPrefix(name, ".git/") {
			return errors.New("workspace contains protected Git metadata")
		}
		result.Files++
		if result.Files > MaxSnapshotFiles {
			return errors.New("snapshot file limit exceeded")
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		header := &tar.Header{Name: name, Mode: int64(info.Mode().Perm()), Format: tar.FormatPAX}
		switch {
		case info.IsDir():
			header.Typeflag = tar.TypeDir
		case info.Mode().IsRegular():
			header.Typeflag = tar.TypeReg
			header.Size = info.Size()
			payload += info.Size()
			if payload > MaxSnapshotBytes {
				return errors.New("snapshot content exceeds 64 MiB")
			}
		case info.Mode()&os.ModeSymlink != 0:
			header.Typeflag = tar.TypeSymlink
			header.Linkname, err = root.Readlink(name)
			if err != nil {
				return err
			}
			if !safeLink(name, header.Linkname) {
				return errors.New("snapshot contains a nonportable external symlink")
			}
		default:
			return errors.New("snapshot contains an unsupported special file")
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg {
			file, err := root.Open(name)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(tw, file, header.Size)
			closeErr := file.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := tw.Close(); err != nil {
		return result, err
	}
	position, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return result, err
	}
	if position > MaxSnapshotBytes+(10<<20) {
		return result, errors.New("snapshot archive exceeds bound")
	}
	result.Bytes = position
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if err := f.Sync(); err != nil {
		return result, err
	}
	if err := f.Close(); err != nil {
		return result, err
	}
	destination := filepath.Join(directory, result.SHA256+".tar")
	if _, err := os.Lstat(destination); err == nil {
		if err := VerifySnapshot(destination, result); err != nil {
			return result, err
		}
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return result, err
	}
	return result, syncDir(directory)
}

func syncDir(directory string) error {
	f, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func VerifySnapshot(filename string, s Snapshot) error {
	if !digestPattern.MatchString(s.SHA256) || s.Bytes < 0 || s.Bytes > MaxSnapshotBytes+(10<<20) || s.Files < 0 || s.Files > MaxSnapshotFiles {
		return errors.New("invalid snapshot manifest")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != s.Bytes {
		return errors.New("snapshot size or file type mismatch")
	}
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, s.Bytes+1)); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != s.SHA256 {
		return errors.New("snapshot checksum mismatch")
	}
	return nil
}

// RestoreSnapshot validates and extracts into a new directory, never over an
// existing workspace. No hard links, special files, traversal, or external links.
func RestoreSnapshot(ctx context.Context, filename, destination string, s Snapshot) (err error) {
	if err := VerifySnapshot(filename, s); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(destination)
		}
	}()
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(io.LimitReader(f, s.Bytes))
	files := 0
	var size int64
	directories := []*tar.Header{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		files++
		if files > MaxSnapshotFiles {
			return errors.New("snapshot file limit exceeded")
		}
		name := header.Name
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") || name == ".git" || strings.HasPrefix(name, ".git/") {
			return errors.New("unsafe archive path")
		}
		parts := strings.Split(path.Dir(name), "/")
		prefix := ""
		for _, part := range parts {
			if part == "." {
				continue
			}
			prefix = path.Join(prefix, part)
			info, e := root.Lstat(prefix)
			if e != nil {
				return fmt.Errorf("archive parent: %w", e)
			}
			if !info.IsDir() {
				return errors.New("archive parent is not a directory")
			}
		}
		mode := os.FileMode(header.Mode) & 0777
		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.Mkdir(name, 0700); err != nil {
				return err
			}
			directories = append(directories, header)
		case tar.TypeReg:
			size += header.Size
			if header.Size < 0 || size > MaxSnapshotBytes {
				return errors.New("snapshot content limit exceeded")
			}
			out, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if e != nil {
				return e
			}
			_, copyErr := io.CopyN(out, tr, header.Size)
			modeErr := out.Chmod(mode)
			syncErr := out.Sync()
			closeErr := out.Close()
			if err := errors.Join(copyErr, modeErr, syncErr, closeErr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if !safeLink(name, header.Linkname) {
				return errors.New("unsafe archive symlink")
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return err
			}
		default:
			return errors.New("unsupported archive entry")
		}
	}
	if files != s.Files {
		return errors.New("snapshot file count mismatch")
	}
	for i := len(directories) - 1; i >= 0; i-- {
		d := directories[i]
		if err := root.Chmod(d.Name, os.FileMode(d.Mode)&0777); err != nil {
			return err
		}
	}
	return syncDir(destination)
}
