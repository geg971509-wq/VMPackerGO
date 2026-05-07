package apkpack

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	elfpacker "github.com/vmpacker/pkg/binary/elf"
)

type Options struct {
	InputAPK    string
	OutputAPK   string
	Library     string
	ABI         string
	Functions   []string
	AddrSpecs   []elfpacker.AddrSpec
	Injector    string
	Profile     string
	Strip       bool
	Debug       bool
	InterpBlob  []byte
	ReportPath  string
	SigningMode string
}

type Report struct {
	InputAPK    string          `json:"input_apk"`
	OutputAPK   string          `json:"output_apk"`
	LibPath     string          `json:"lib_path"`
	ABI         string          `json:"abi"`
	PackedSO    string          `json:"packed_so"`
	UnsignedAPK string          `json:"unsigned_apk,omitempty"`
	AlignedAPK  string          `json:"aligned_apk,omitempty"`
	Signing     SigningReport   `json:"signing"`
	ELFReport   json.RawMessage `json:"elf_report,omitempty"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
}

type SigningReport struct {
	Mode     string `json:"mode"`
	Keystore string `json:"keystore,omitempty"`
}

func Pack(opts Options) (retErr error) {
	if opts.ABI == "" {
		opts.ABI = "arm64-v8a"
	}
	if opts.SigningMode == "" {
		opts.SigningMode = "debug"
	}
	libPath := normalizeLibPath(opts.Library, opts.ABI)
	if opts.InputAPK == "" || opts.OutputAPK == "" || opts.Library == "" {
		return fmt.Errorf("apk mode requires input APK, -lib, and -o output APK")
	}
	if len(opts.Functions) == 0 && len(opts.AddrSpecs) == 0 {
		return fmt.Errorf("apk mode requires -func or -addr for the selected library")
	}
	if opts.SigningMode != "debug" && opts.SigningMode != "none" {
		return fmt.Errorf("unsupported apk signing mode %q (supported: debug, none)", opts.SigningMode)
	}

	report := &Report{
		InputAPK:  opts.InputAPK,
		OutputAPK: opts.OutputAPK,
		LibPath:   libPath,
		ABI:       opts.ABI,
		Signing:   SigningReport{Mode: opts.SigningMode},
		Status:    "running",
	}
	defer func() {
		if opts.ReportPath == "" {
			return
		}
		if retErr != nil {
			report.Status = "failed"
			report.Error = retErr.Error()
		} else {
			report.Status = "ok"
		}
		if err := writeJSON(opts.ReportPath, report); err != nil && retErr == nil {
			retErr = err
		}
	}()

	tmp, err := os.MkdirTemp("", "vmpacker-apk-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	origSO := filepath.Join(tmp, "original.so")
	if err := extractAPKEntry(opts.InputAPK, libPath, origSO); err != nil {
		return err
	}

	packedSO := filepath.Join(tmp, "protected.so")
	elfReportPath := filepath.Join(tmp, "elf-report.json")
	p := elfpacker.NewPackerWithTarget(origSO, packedSO, opts.Functions, opts.AddrSpecs, false, opts.Strip, opts.Debug, "android", opts.InterpBlob)
	if err := p.Configure(elfpacker.PackerOptions{
		AndroidMode: "so",
		Injector:    opts.Injector,
		Profile:     opts.Profile,
		ReportPath:  elfReportPath,
	}); err != nil {
		return err
	}
	if err := p.Process(); err != nil {
		return err
	}
	report.PackedSO = libPath
	if data, err := os.ReadFile(elfReportPath); err == nil {
		report.ELFReport = sanitizeEmbeddedELFReport(data, libPath)
	}

	unsigned := filepath.Join(tmp, "unsigned.apk")
	if err := replaceAPKEntry(opts.InputAPK, libPath, packedSO, unsigned); err != nil {
		return err
	}

	aligned := filepath.Join(tmp, "aligned.apk")
	if err := zipalign(unsigned, aligned); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(opts.OutputAPK), 0755); err != nil && filepath.Dir(opts.OutputAPK) != "." {
		return err
	}
	switch opts.SigningMode {
	case "none":
		return copyFile(aligned, opts.OutputAPK)
	case "debug":
		ks, err := ensureDebugKeystore()
		if err != nil {
			return err
		}
		report.Signing.Keystore = ks
		return apksign(aligned, opts.OutputAPK, ks)
	default:
		return fmt.Errorf("unsupported apk signing mode %q", opts.SigningMode)
	}
}

func normalizeLibPath(lib, abi string) string {
	lib = strings.TrimPrefix(filepath.ToSlash(lib), "/")
	if strings.HasPrefix(lib, "lib/") {
		return lib
	}
	if strings.Contains(lib, "/") {
		return "lib/" + lib
	}
	return "lib/" + abi + "/" + lib
}

func extractAPKEntry(apkPath, entryName, outPath string) error {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != entryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(outPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("library %q not found in APK", entryName)
}

func replaceAPKEntry(apkPath, entryName, replacementPath, outPath string) error {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()

	replaced := false
	for _, f := range zr.File {
		if f.Name == entryName {
			if err := addFileToZip(zw, f.FileHeader, entryName, replacementPath); err != nil {
				return err
			}
			replaced = true
			continue
		}
		if isStaleAPKSignatureEntry(f.Name) {
			continue
		}
		if err := copyZipEntry(zw, f); err != nil {
			return err
		}
	}
	if !replaced {
		return fmt.Errorf("library %q not found in APK", entryName)
	}
	return nil
}

func copyZipEntry(zw *zip.Writer, f *zip.File) error {
	h := f.FileHeader
	w, err := zw.CreateHeader(&h)
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(w, rc)
	return err
}

func addFileToZip(zw *zip.Writer, base zip.FileHeader, name, path string) error {
	h := base
	h.Name = name
	h.CRC32 = 0
	h.CompressedSize = 0
	h.CompressedSize64 = 0
	h.UncompressedSize = 0
	h.UncompressedSize64 = 0
	var storedData []byte
	if h.Method == zip.Store {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		storedData = data
		h.CRC32 = crc32.ChecksumIEEE(data)
		h.CompressedSize = uint32(len(data))
		h.CompressedSize64 = uint64(len(data))
		h.UncompressedSize = uint32(len(data))
		h.UncompressedSize64 = uint64(len(data))
	}
	w, err := zw.CreateHeader(&h)
	if err != nil {
		return err
	}
	if storedData != nil {
		_, err = w.Write(storedData)
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}

func isStaleAPKSignatureEntry(name string) bool {
	upper := strings.ToUpper(name)
	if !strings.HasPrefix(upper, "META-INF/") {
		return false
	}
	return strings.HasSuffix(upper, ".RSA") ||
		strings.HasSuffix(upper, ".DSA") ||
		strings.HasSuffix(upper, ".EC") ||
		strings.HasSuffix(upper, ".SF") ||
		upper == "META-INF/MANIFEST.MF"
}

func zipalign(in, out string) error {
	tool, err := androidBuildTool("zipalign")
	if err != nil {
		return err
	}
	return run(tool, "-f", "-p", "4", in, out)
}

func apksign(in, out, keystore string) error {
	tool, err := androidBuildTool("apksigner")
	if err != nil {
		return err
	}
	if err := run(tool, "sign", "--ks", keystore, "--ks-pass", "pass:android", "--key-pass", "pass:android", "--out", out, in); err != nil {
		return err
	}
	return run(tool, "verify", out)
}

func androidBuildTool(name string) (string, error) {
	if bt := os.Getenv("ANDROID_BUILD_TOOLS"); bt != "" {
		p := filepath.Join(bt, name)
		if isExecutable(p) {
			return p, nil
		}
	}
	sdk := os.Getenv("ANDROID_HOME")
	if sdk == "" {
		home, _ := os.UserHomeDir()
		sdk = filepath.Join(home, "Library", "Android", "sdk")
	}
	matches, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*", name))
	for i := len(matches) - 1; i >= 0; i-- {
		if isExecutable(matches[i]) {
			return matches[i], nil
		}
	}
	return "", fmt.Errorf("missing Android build tool %s; set ANDROID_HOME or ANDROID_BUILD_TOOLS", name)
}

func ensureDebugKeystore() (string, error) {
	if ks := os.Getenv("ANDROID_DEBUG_KEYSTORE"); ks != "" {
		if _, err := os.Stat(ks); err == nil {
			return ks, nil
		}
	}
	home, _ := os.UserHomeDir()
	ks := filepath.Join(home, ".android", "debug.keystore")
	if _, err := os.Stat(ks); err == nil {
		return ks, nil
	}
	if err := os.MkdirAll(filepath.Dir(ks), 0755); err != nil {
		return "", err
	}
	if err := run("keytool", "-genkeypair", "-keystore", ks, "-storepass", "android", "-keypass", "android", "-alias", "androiddebugkey", "-keyalg", "RSA", "-keysize", "2048", "-validity", "10000", "-dname", "CN=Android Debug,O=Android,C=US"); err != nil {
		return "", err
	}
	return ks, nil
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0111 != 0
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %v", name, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func sanitizeEmbeddedELFReport(data []byte, libPath string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return json.RawMessage(data)
	}
	m["input"] = libPath
	m["output"] = libPath
	out, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(data)
	}
	return json.RawMessage(out)
}
