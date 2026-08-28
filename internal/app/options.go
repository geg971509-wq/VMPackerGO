package app

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vmpacker/internal/abi"
	elfpacker "github.com/vmpacker/internal/elf"
)

const maxInputSize int64 = 1 << 30
const ndkRevision = "29.0.14206865"

type options struct {
	Mode      string
	Func      string
	Addr      string
	Output    string
	NDK       string
	Report    string
	DebugMap  string
	Strip     bool
	Force     bool
	Seed      string
	Manifest  string
	ABI       string
	Info      bool
	Verbose   bool
	Version   bool
	Input     string
	InputMode os.FileMode
	Funcs     []string
	AddrSpecs []elfpacker.AddrSpec
	Selected  []selectedFunction
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("vmpacker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.Mode, "mode", "auto", "Android ELF mode: auto, so, or native")
	fs.StringVar(&opts.Func, "func", "", "protect one function by name")
	fs.StringVar(&opts.Addr, "addr", "", "protect one address or range: 0xADDR or 0xSTART-0xEND[:name]")
	fs.StringVar(&opts.Output, "o", "", "output ELF path (default: input+.vmp)")
	fs.StringVar(&opts.NDK, "ndk", os.Getenv("ANDROID_NDK_HOME"), "Android NDK r29 path for later runtime compilation")
	fs.StringVar(&opts.Report, "report", "", "write report schema v1 JSON")
	fs.StringVar(&opts.DebugMap, "debug-map", "", "write ARM64-to-VM debug mapping")
	fs.BoolVar(&opts.Strip, "strip", true, "strip the static symbol table")
	fs.BoolVar(&opts.Force, "force", false, "atomically replace existing destinations")
	fs.StringVar(&opts.Seed, "seed", "", "64 hexadecimal characters; reserved for Phase 4 debug/test use")
	fs.StringVar(&opts.Manifest, "manifest", "", "manifest v1 JSON for one or more functions")
	fs.StringVar(&opts.ABI, "abi", "", "direct entry ABI, for example i32(ptr,u64)")
	fs.BoolVar(&opts.Info, "info", false, "print Android ELF information without transforming")
	fs.BoolVar(&opts.Verbose, "v", false, "print verbose transformation details")
	fs.BoolVar(&opts.Version, "version", false, "print version and commit")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vmpacker [options] <input-android-elf>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.Version {
		if fs.NArg() != 0 {
			return opts, fmt.Errorf("-version does not accept an input")
		}
		return opts, nil
	}
	if fs.NArg() != 1 {
		return opts, fmt.Errorf("exactly one positional Android ELF input is required")
	}
	opts.Input = fs.Arg(0)
	if opts.Output == "" {
		opts.Output = opts.Input + ".vmp"
	}
	if opts.Mode != "auto" && opts.Mode != "so" && opts.Mode != "native" {
		return opts, fmt.Errorf("invalid -mode %q; expected auto, so, or native", opts.Mode)
	}
	if opts.Seed != "" {
		if len(opts.Seed) != 64 {
			return opts, fmt.Errorf("-seed must contain exactly 64 hexadecimal characters")
		}
		if _, err := hex.DecodeString(opts.Seed); err != nil {
			return opts, fmt.Errorf("-seed must contain exactly 64 hexadecimal characters")
		}
		return opts, fmt.Errorf("-seed is reserved for Phase 4 and is not accepted by the current fixed runtime")
	}
	if err := validateNDK(opts.NDK); err != nil {
		return opts, err
	}
	if opts.Info {
		return opts, nil
	}

	direct := strings.TrimSpace(opts.Func) != "" || strings.TrimSpace(opts.Addr) != ""
	if direct && opts.Manifest != "" {
		return opts, fmt.Errorf("direct selection and -manifest are mutually exclusive")
	}
	if !direct && opts.Manifest == "" {
		return opts, fmt.Errorf("select one function with -func/-addr or provide -manifest")
	}
	if opts.Manifest != "" {
		if opts.ABI != "" {
			return opts, fmt.Errorf("-abi is only valid with direct selection; manifest entries contain their own ABI")
		}
		var err error
		opts.Funcs, opts.AddrSpecs, opts.Selected, err = loadManifest(opts.Manifest)
		return opts, err
	}
	if opts.ABI == "" {
		return opts, fmt.Errorf("a directly selected function or range requires -abi")
	}
	if strings.Contains(opts.Func, ",") || strings.Contains(opts.Addr, ",") {
		return opts, fmt.Errorf("multiple direct selections are not allowed; use -manifest")
	}
	if strings.TrimSpace(opts.Func) != "" && strings.TrimSpace(opts.Addr) != "" {
		return opts, fmt.Errorf("select exactly one direct function or address; use -manifest for multiple functions")
	}
	sig, err := abi.Parse(opts.ABI)
	if err != nil {
		return opts, err
	}
	if opts.Func != "" {
		raw := opts.Func
		opts.Func = strings.TrimSpace(opts.Func)
		opts.Funcs = []string{opts.Func}
		opts.Selected = []selectedFunction{{Source: "direct", Selector: raw, Name: opts.Func, ABI: sig}}
	} else {
		raw := opts.Addr
		opts.Addr = strings.TrimSpace(opts.Addr)
		spec, err := elfpacker.ParseAddrSpec(opts.Addr)
		if err != nil {
			return opts, fmt.Errorf("invalid -addr: %w", err)
		}
		opts.AddrSpecs = []elfpacker.AddrSpec{spec}
		selected := selectedFunction{Source: "direct", Selector: raw, Name: spec.Name, ABI: sig}
		if spec.End == 0 {
			selected.Address = fmt.Sprintf("0x%x", spec.Addr)
		} else {
			selected.Range = fmt.Sprintf("0x%x-0x%x", spec.Addr, spec.End)
		}
		opts.Selected = []selectedFunction{selected}
	}
	return opts, nil
}

func validateNDK(path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(path, "source.properties"))
	if err != nil {
		return fmt.Errorf("read Android NDK source.properties: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "Pkg.Revision" {
			if strings.TrimSpace(parts[1]) != ndkRevision {
				return fmt.Errorf("Android NDK revision %s is required; found %q", ndkRevision, strings.TrimSpace(parts[1]))
			}
			return nil
		}
	}
	return fmt.Errorf("Android NDK source.properties does not contain Pkg.Revision")
}

type openedInput interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

func readBoundedInput(path string) ([]byte, os.FileMode, error) {
	return readBoundedInputWith(path, func(path string) (openedInput, error) { return os.Open(path) })
}

func readBoundedInputWith(path string, open func(string) (openedInput, error)) ([]byte, os.FileMode, error) {
	file, err := open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open input: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("stat opened input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("input must be a regular file")
	}
	if info.Size() > maxInputSize {
		return nil, 0, fmt.Errorf("input exceeds the 1 GiB limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxInputSize+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read input: %w", err)
	}
	if int64(len(data)) > maxInputSize {
		return nil, 0, fmt.Errorf("input exceeds the 1 GiB limit")
	}
	return data, info.Mode(), nil
}

type checkedPath struct {
	name        string
	raw         string
	canonical   string
	destination bool
	lstat       os.FileInfo
	stat        os.FileInfo
}

func validatePaths(opts options) error {
	paths := []checkedPath{
		{name: "input", raw: opts.Input},
		{name: "output", raw: opts.Output, destination: true},
	}
	if opts.Manifest != "" {
		paths = append(paths, checkedPath{name: "manifest", raw: opts.Manifest})
	}
	if opts.Report != "" {
		paths = append(paths, checkedPath{name: "report", raw: opts.Report, destination: true})
	}
	if opts.DebugMap != "" {
		paths = append(paths, checkedPath{name: "debug map", raw: opts.DebugMap, destination: true})
	}

	for i := range paths {
		if err := resolveCheckedPath(&paths[i]); err != nil {
			return err
		}
	}
	for i := range paths {
		for j := i + 1; j < len(paths); j++ {
			if paths[i].canonical == paths[j].canonical {
				return fmt.Errorf("%s and %s paths must be distinct", paths[i].name, paths[j].name)
			}
			if paths[i].stat != nil && paths[j].stat != nil && os.SameFile(paths[i].stat, paths[j].stat) {
				return fmt.Errorf("%s and %s paths refer to the same file", paths[i].name, paths[j].name)
			}
		}
	}

	existingDestinations := 0
	for _, path := range paths {
		if !path.destination || path.lstat == nil {
			continue
		}
		existingDestinations++
		if !opts.Force {
			return fmt.Errorf("%s exists; use -force to replace it", path.name)
		}
		if path.lstat.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("-force refuses symlink %s destination", path.name)
		}
		if !path.lstat.Mode().IsRegular() {
			return fmt.Errorf("-force requires regular-file %s destination", path.name)
		}
	}
	if opts.Force && existingDestinations > 1 {
		return fmt.Errorf("-force cannot replace multiple existing destinations in one publish")
	}
	return nil
}

func resolveCheckedPath(path *checkedPath) error {
	abs, err := filepath.Abs(filepath.Clean(path.raw))
	if err != nil {
		return fmt.Errorf("resolve %s path: %w", path.name, err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return fmt.Errorf("resolve %s parent directory: %w", path.name, err)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("%s parent directory: %w", path.name, err)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("%s parent is not a directory", path.name)
	}
	path.canonical = filepath.Join(parent, filepath.Base(abs))
	path.lstat, err = os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		path.lstat = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path.name, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if path.destination && path.lstat.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return fmt.Errorf("resolve %s path: %w", path.name, err)
	}
	path.canonical = resolved
	path.stat, err = os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path.name, err)
	}
	return nil
}

func artifactMode(input os.FileMode) os.FileMode {
	mode := input.Perm()
	if mode&0111 != 0 {
		return mode
	}
	return mode &^ 0111
}
