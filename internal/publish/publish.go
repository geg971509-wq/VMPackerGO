package publish

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type File struct {
	Path     string
	Data     []byte
	Mode     os.FileMode
	Artifact bool
}

type fileOps struct {
	createTemp func(string, string) (*os.File, error)
	write      func(io.Writer, []byte) (int, error)
	chmod      func(*os.File, os.FileMode) error
	syncFile   func(*os.File) error
	close      func(*os.File) error
	lstat      func(string) (os.FileInfo, error)
	link       func(string, string) error
	rename     func(string, string) error
	remove     func(string) error
	syncDir    func(string) error
}

var ops = fileOps{
	createTemp: os.CreateTemp,
	write:      func(w io.Writer, data []byte) (int, error) { return w.Write(data) },
	chmod:      func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) },
	syncFile:   func(f *os.File) error { return f.Sync() },
	close:      func(f *os.File) error { return f.Close() },
	lstat:      os.Lstat,
	link:       os.Link,
	rename:     os.Rename,
	remove:     os.Remove,
	syncDir:    syncDirectory,
}

type prepared struct {
	file    File
	temp    string
	backup  string
	existed bool
}

// All prepares sibling temporary files, publishes auxiliaries first, and the
// artifact last as the success marker. It is not a cross-filesystem transaction.
func All(files []File, force bool) error {
	if len(files) == 0 {
		return nil
	}
	ordered := artifactLast(files)
	existing, err := preflight(ordered, force)
	if err != nil {
		return err
	}

	items := make([]prepared, 0, len(ordered))
	for i, file := range ordered {
		item, prepareErr := prepare(file)
		if prepareErr != nil {
			return errors.Join(prepareErr, cleanupTemps(items))
		}
		item.existed = existing[i]
		items = append(items, item)
	}

	published := make([]*prepared, 0, len(items))
	for i := range items {
		if publishErr := publishOne(&items[i], force); publishErr != nil {
			return errors.Join(
				fmt.Errorf("publish %s: %w", items[i].file.Path, publishErr),
				cleanupTemps(items[i:]),
				rollback(published),
			)
		}
		published = append(published, &items[i])
	}
	for _, item := range published {
		if err := cleanupBackup(item); err != nil {
			return errors.Join(err, rollback(published))
		}
	}
	return nil
}

func artifactLast(files []File) []File {
	ordered := append([]File(nil), files...)
	for i := range ordered {
		if ordered[i].Artifact && i != len(ordered)-1 {
			artifact := ordered[i]
			copy(ordered[i:], ordered[i+1:])
			ordered[len(ordered)-1] = artifact
			break
		}
	}
	return ordered
}

func preflight(files []File, force bool) ([]bool, error) {
	existing := make([]bool, len(files))
	existingCount := 0
	for i, file := range files {
		info, err := ops.lstat(file.Path)
		switch {
		case err == nil:
			existing[i] = true
			existingCount++
			if !force {
				return nil, fmt.Errorf("destination exists; use -force to replace it: %s", file.Path)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("-force refuses symlink destination: %s", file.Path)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("-force requires a regular-file destination: %s", file.Path)
			}
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, fmt.Errorf("stat destination %s: %w", file.Path, err)
		}
	}
	if force && existingCount > 1 {
		return nil, fmt.Errorf("-force cannot replace multiple existing destinations in one publish")
	}
	return existing, nil
}

func prepare(file File) (prepared, error) {
	dir := filepath.Dir(file.Path)
	f, err := ops.createTemp(dir, "."+filepath.Base(file.Path)+".tmp-*")
	if err != nil {
		return prepared{}, fmt.Errorf("create temporary file for %s: %w", file.Path, err)
	}
	item := prepared{file: file, temp: f.Name()}
	fail := func(operationErr error) (prepared, error) {
		return prepared{}, errors.Join(
			operationErr,
			wrapErr("close temporary file "+item.temp, ops.close(f)),
			removePath("temporary file", item.temp),
		)
	}
	if err := ops.chmod(f, file.Mode.Perm()); err != nil {
		return fail(fmt.Errorf("chmod temporary file for %s: %w", file.Path, err))
	}
	if n, writeErr := ops.write(f, file.Data); writeErr != nil || n != len(file.Data) {
		if n != len(file.Data) {
			writeErr = errors.Join(writeErr, io.ErrShortWrite)
		}
		return fail(fmt.Errorf("write temporary file for %s: %w", file.Path, writeErr))
	}
	if err := ops.syncFile(f); err != nil {
		return fail(fmt.Errorf("sync temporary file for %s: %w", file.Path, err))
	}
	if err := ops.close(f); err != nil {
		return prepared{}, errors.Join(
			fmt.Errorf("close temporary file for %s: %w", file.Path, err),
			removePath("temporary file", item.temp),
		)
	}
	return item, nil
}

func publishOne(item *prepared, force bool) error {
	dir := filepath.Dir(item.file.Path)
	if force && item.existed {
		if err := createBackup(item); err != nil {
			return err
		}
	}

	if force {
		if err := ops.rename(item.temp, item.file.Path); err != nil {
			return errors.Join(err, cleanupBackup(item))
		}
		item.temp = ""
	} else {
		if err := ops.link(item.temp, item.file.Path); err != nil {
			return err
		}
		if err := removePath("temporary file", item.temp); err != nil {
			return errors.Join(err, restore(item), removePath("temporary file", item.temp))
		}
		item.temp = ""
	}
	if err := ops.syncDir(dir); err != nil {
		return errors.Join(fmt.Errorf("sync destination directory %s: %w", dir, err), restore(item))
	}
	return nil
}

func createBackup(item *prepared) error {
	dir := filepath.Dir(item.file.Path)
	f, err := ops.createTemp(dir, "."+filepath.Base(item.file.Path)+".backup-*")
	if err != nil {
		return fmt.Errorf("create rollback path for %s: %w", item.file.Path, err)
	}
	backup := f.Name()
	if closeErr := ops.close(f); closeErr != nil {
		return errors.Join(
			fmt.Errorf("close rollback path %s: %w", backup, closeErr),
			removePath("rollback path", backup),
		)
	}
	if err := removePath("rollback path", backup); err != nil {
		return err
	}
	if err := ops.link(item.file.Path, backup); err != nil {
		return fmt.Errorf("create rollback snapshot %s: %w", backup, err)
	}
	item.backup = backup
	if err := ops.syncDir(dir); err != nil {
		return fmt.Errorf("sync rollback snapshot %s: %w; snapshot retained", backup, err)
	}
	return nil
}

func cleanupTemps(items []prepared) error {
	var errs []error
	for i := range items {
		if items[i].temp != "" {
			errs = append(errs, removePath("temporary file", items[i].temp))
		}
	}
	return errors.Join(errs...)
}

func cleanupBackup(item *prepared) error {
	if item.backup == "" {
		return nil
	}
	backup := item.backup
	if err := ops.syncDir(filepath.Dir(item.file.Path)); err != nil {
		return fmt.Errorf("sync before rollback snapshot cleanup %s: %w; snapshot retained", backup, err)
	}
	if err := removePath("rollback snapshot", backup); err != nil {
		return fmt.Errorf("%w; rollback snapshot retained at %s", err, backup)
	}
	item.backup = ""
	return nil
}

func rollback(items []*prepared) error {
	var errs []error
	for i := len(items) - 1; i >= 0; i-- {
		errs = append(errs, restore(items[i]))
	}
	return errors.Join(errs...)
}

func restore(item *prepared) error {
	dir := filepath.Dir(item.file.Path)
	if item.backup != "" {
		backup := item.backup
		if err := ops.rename(backup, item.file.Path); err != nil {
			return fmt.Errorf("restore %s from rollback snapshot %s: %w; snapshot retained", item.file.Path, backup, err)
		}
		if err := ops.syncDir(dir); err != nil {
			relinkErr := ops.link(item.file.Path, backup)
			if relinkErr != nil {
				relinkErr = fmt.Errorf("retain rollback snapshot %s: %w", backup, relinkErr)
			}
			return errors.Join(fmt.Errorf("sync restored destination directory %s: %w; rollback snapshot retained at %s", dir, err, backup), relinkErr)
		}
		item.backup = ""
		return nil
	}

	removeErr := removePath("published destination", item.file.Path)
	syncErr := ops.syncDir(dir)
	if syncErr != nil {
		syncErr = fmt.Errorf("sync rolled-back destination directory %s: %w", dir, syncErr)
	}
	return errors.Join(removeErr, syncErr)
}

func removePath(kind, path string) error {
	if path == "" {
		return nil
	}
	if err := ops.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s %s: %w", kind, path, err)
	}
	return nil
}

func wrapErr(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	if errors.Is(syncErr, syscall.EINVAL) || errors.Is(syncErr, syscall.ENOTSUP) {
		syncErr = nil
	}
	return errors.Join(syncErr, dir.Close())
}
