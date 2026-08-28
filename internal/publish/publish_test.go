package publish

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoClobberForcePermissionsAndCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600, Artifact: true}}, false); err == nil {
		t.Fatal("no-clobber replaced file")
	}
	assertFile(t, path, "old")
	if err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600, Artifact: true}}, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "new")
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	assertNoWorkFiles(t, dir)
}

func TestForceRejectsMultipleExistingAndSymlinkDestinations(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := All([]File{{Path: a, Data: []byte("x")}, {Path: b, Data: []byte("y"), Artifact: true}}, true); err == nil {
		t.Fatal("accepted multiple force replacements")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	if err := All([]File{{Path: link, Data: []byte("x"), Artifact: true}}, true); err == nil {
		t.Fatal("accepted force symlink destination")
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := All([]File{{Path: dangling, Data: []byte("x"), Artifact: true}}, true); err == nil {
		t.Fatal("accepted dangling force symlink destination")
	}
}

func TestForceRenameNeverRemovesLiveDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	withOps(t, func() {
		realRename := ops.rename
		ops.rename = func(from, to string) error {
			if to == path {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("destination missing before atomic rename: %v", err)
				}
			}
			return realRename(from, to)
		}
		if err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600, Artifact: true}}, true); err != nil {
			t.Fatal(err)
		}
	})
	assertFile(t, path, "new")
}

func TestPreflightStatFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	withOps(t, func() {
		sentinel := errors.New("lstat")
		ops.lstat = func(string) (os.FileInfo, error) { return nil, sentinel }
		if err := All([]File{{Path: path, Data: []byte("new")}}, false); !errors.Is(err, sentinel) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestPrepareFailuresAggregateCleanup(t *testing.T) {
	cases := map[string]func(error){
		"createTemp": func(sentinel error) {
			ops.createTemp = func(string, string) (*os.File, error) { return nil, sentinel }
		},
		"chmod": func(sentinel error) {
			ops.chmod = func(*os.File, os.FileMode) error { return sentinel }
		},
		"write": func(sentinel error) {
			ops.write = func(io.Writer, []byte) (int, error) { return 0, sentinel }
		},
		"short-write": func(error) {
			ops.write = func(io.Writer, []byte) (int, error) { return 0, nil }
		},
		"file-sync": func(sentinel error) {
			ops.syncFile = func(*os.File) error { return sentinel }
		},
		"close": func(sentinel error) {
			ops.close = func(f *os.File) error { return errors.Join(f.Close(), sentinel) }
		},
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "out")
			withOps(t, func() {
				sentinel := errors.New(name)
				inject(sentinel)
				err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600, Artifact: true}}, false)
				if err == nil || (name != "short-write" && !errors.Is(err, sentinel)) || (name == "short-write" && !errors.Is(err, io.ErrShortWrite)) {
					t.Fatalf("err=%v", err)
				}
			})
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("destination exists: %v", err)
			}
		})
	}
}

func TestPrepareCleanupErrorsAreJoined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	withOps(t, func() {
		writeErr := errors.New("write")
		closeErr := errors.New("close")
		removeErr := errors.New("remove")
		ops.write = func(io.Writer, []byte) (int, error) { return 0, writeErr }
		ops.close = func(f *os.File) error { return errors.Join(f.Close(), closeErr) }
		ops.remove = func(string) error { return removeErr }
		err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600}}, false)
		for _, want := range []error{writeErr, closeErr, removeErr} {
			if !errors.Is(err, want) {
				t.Fatalf("missing %v in %v", want, err)
			}
		}
	})
}

func TestPublicationFailuresRollback(t *testing.T) {
	for _, name := range []string{"link", "rename", "temp-remove", "directory-sync", "backup-create", "backup-close", "backup-placeholder-remove", "backup-link", "backup-sync"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "out")
			force := name == "rename" || strings.HasPrefix(name, "backup-")
			if force {
				if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			withOps(t, func() {
				sentinel := errors.New(name)
				switch name {
				case "link":
					ops.link = func(string, string) error { return sentinel }
				case "rename":
					ops.rename = func(string, string) error { return sentinel }
				case "temp-remove":
					realRemove := ops.remove
					failed := false
					ops.remove = func(path string) error {
						if !failed && strings.Contains(path, ".tmp-") {
							failed = true
							return sentinel
						}
						return realRemove(path)
					}
				case "directory-sync":
					calls := 0
					ops.syncDir = func(string) error {
						calls++
						if calls == 1 {
							return sentinel
						}
						return nil
					}
				case "backup-create":
					realCreate := ops.createTemp
					calls := 0
					ops.createTemp = func(dir, pattern string) (*os.File, error) {
						calls++
						if calls == 2 {
							return nil, sentinel
						}
						return realCreate(dir, pattern)
					}
				case "backup-close":
					realClose := ops.close
					closeCalls := 0
					ops.close = func(file *os.File) error {
						closeCalls++
						err := realClose(file)
						if closeCalls == 2 {
							return errors.Join(err, sentinel)
						}
						return err
					}
				case "backup-placeholder-remove":
					realRemove := ops.remove
					ops.remove = func(path string) error {
						if strings.Contains(path, ".backup-") {
							return sentinel
						}
						return realRemove(path)
					}
				case "backup-link":
					ops.link = func(string, string) error { return sentinel }
				case "backup-sync":
					ops.syncDir = func(string) error { return sentinel }
				}
				err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600, Artifact: true}}, force)
				if err == nil || !errors.Is(err, sentinel) {
					t.Fatalf("err=%v", err)
				}
			})
			if force {
				assertFile(t, path, "old")
			} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("destination exists: %v", err)
			}
		})
	}
}

func TestRestoreAndFinalCleanupFailures(t *testing.T) {
	for _, name := range []string{"restore-rename", "restore-sync", "final-backup-remove", "final-backup-sync"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "out")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			withOps(t, func() {
				sentinel := errors.New(name)
				switch name {
				case "restore-rename":
					renameCalls := 0
					realRename := ops.rename
					ops.rename = func(from, to string) error {
						renameCalls++
						if renameCalls == 2 {
							return sentinel
						}
						return realRename(from, to)
					}
					syncCalls := 0
					ops.syncDir = func(string) error {
						syncCalls++
						if syncCalls == 2 {
							return errors.New("publish-sync")
						}
						return nil
					}
				case "restore-sync":
					syncCalls := 0
					ops.syncDir = func(string) error {
						syncCalls++
						if syncCalls == 2 || syncCalls == 3 {
							return sentinel
						}
						return nil
					}
				case "final-backup-remove":
					realRemove := ops.remove
					ops.remove = func(path string) error {
						if strings.Contains(path, ".backup-") {
							if _, err := os.Stat(path); err == nil {
								return sentinel
							}
						}
						return realRemove(path)
					}
				case "final-backup-sync":
					syncCalls := 0
					ops.syncDir = func(string) error {
						syncCalls++
						if syncCalls == 3 {
							return sentinel
						}
						return nil
					}
				}
				err := All([]File{{Path: path, Data: []byte("new"), Mode: 0600, Artifact: true}}, true)
				if err == nil || !errors.Is(err, sentinel) {
					t.Fatalf("err=%v", err)
				}
				if (name == "restore-rename" || name == "restore-sync") && !strings.Contains(err.Error(), ".backup-") {
					t.Fatalf("missing retained backup path: %v", err)
				}
			})
			if name == "final-backup-remove" || name == "final-backup-sync" {
				assertFile(t, path, "old")
			}
		})
	}
}

func TestMultiAuxiliaryRollbackAggregatesFailures(t *testing.T) {
	dir := t.TempDir()
	debugPath := filepath.Join(dir, "debug")
	reportPath := filepath.Join(dir, "report")
	artifactPath := filepath.Join(dir, "artifact")
	withOps(t, func() {
		publishErr := errors.New("artifact-link")
		removeErr := errors.New("rollback-remove")
		syncErr := errors.New("rollback-sync")
		realLink, realRemove := ops.link, ops.remove
		linkCalls := 0
		ops.link = func(from, to string) error {
			linkCalls++
			if linkCalls == 3 {
				return publishErr
			}
			return realLink(from, to)
		}
		ops.remove = func(path string) error {
			if path == reportPath {
				return removeErr
			}
			return realRemove(path)
		}
		syncCalls := 0
		ops.syncDir = func(string) error {
			syncCalls++
			if syncCalls == 4 {
				return syncErr
			}
			return nil
		}
		err := All([]File{
			{Path: artifactPath, Data: []byte("artifact"), Mode: 0600, Artifact: true},
			{Path: debugPath, Data: []byte("debug"), Mode: 0600},
			{Path: reportPath, Data: []byte("report"), Mode: 0600},
		}, false)
		for _, want := range []error{publishErr, removeErr, syncErr} {
			if !errors.Is(err, want) {
				t.Fatalf("missing %v in %v", want, err)
			}
		}
	})
	if _, err := os.Stat(debugPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("debug still exists: %v", err)
	}
	if _, err := os.Stat(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact exists: %v", err)
	}
}

func withOps(t *testing.T, fn func()) {
	t.Helper()
	old := ops
	t.Cleanup(func() { ops = old })
	fn()
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s: data=%q err=%v", path, data, err)
	}
}

func assertNoWorkFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") || strings.Contains(entry.Name(), ".backup-") {
			t.Fatalf("work file left: %s", entry.Name())
		}
	}
}
