package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/vmpacker/pkg/apkpack"
	elfpacker "github.com/vmpacker/pkg/binary/elf"
)

// ============================================================
// vmpacker - ARM64 ELF VMP 保护工具 (模块化版本)
//
// 用法:
//   vmpacker -func check_license [-v] [-o output] input.elf
//   vmpacker -info input.elf
//
// 功能:
//   读取编译好的 ARM64 ELF，解码指定函数的 ARM64 指令，
//   翻译为自定义 VM 字节码，替换原函数为 VM 跳板。
// ============================================================

//go:embed vm_interp.bin
var interpBlob []byte

func main() {
	funcList := flag.String("func", "", "要保护的函数名（逗号分隔多个）")
	addrList := flag.String("addr", "", "按地址保护（格式: 0xADDR[:name] 或 0xSTART-0xEND[:name]，逗号分隔多个）")
	output := flag.String("o", "", "输出文件路径（默认: 原文件名.vmp）")
	verbose := flag.Bool("v", false, "详细输出（显示反汇编）")
	strip := flag.Bool("strip", true, "清除符号表（防止strip破坏保护）")
	debug := flag.Bool("debug", false, "生成 debug 对照文件（ARM64 → VM 字节码映射）")
	_ = flag.Bool("token", true, "兼容旧命令；当前固定启用 Token 化入口模式")
	_ = flag.Bool("force", false, "兼容 nainiu pack_entry；当前忽略")
	_ = flag.String("ndk", "", "兼容 nainiu pack_entry；当前忽略")
	compatMode := flag.String("mode", "", "兼容 nainiu pack_entry: so|native，等价于 -android-mode")
	targetOS := flag.String("target", "linux", "目标运行环境: linux 或 android（android 用于 APK 内 arm64-v8a JNI .so）")
	androidMode := flag.String("android-mode", "auto", "Android artifact 模式: auto, so, native")
	injector := flag.String("injector", "auto", "注入策略: auto, note, add-segment")
	profile := flag.String("profile", "balanced", "保护/兼容配置: compat, balanced, strong")
	reportPath := flag.String("report", "", "输出 JSON pack 报告（策略选择、函数、payload 摘要）")
	apkPath := flag.String("apk", "", "APK 工作流输入：解包并保护指定 lib/<abi>/*.so")
	apkLib := flag.String("lib", "", "APK 工作流要保护的库名或路径，如 libdemo.so / arm64-v8a/libdemo.so / lib/arm64-v8a/libdemo.so")
	apkABI := flag.String("abi", "arm64-v8a", "APK 工作流 ABI，默认 arm64-v8a")
	apkSign := flag.String("apk-sign", "debug", "APK 签名模式: debug 或 none")
	info := flag.Bool("info", false, "仅打印 ELF 信息，不做保护")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `vmpacker - ARM64 ELF VMP 保护工具

用法:
  vmpacker -func <函数名> [-v] [-o output] <input.elf>
  vmpacker -addr <0xSTART-0xEND[:名称]> [-v] [-o output] <input.elf>
  vmpacker -apk <input.apk> -lib <libfoo.so> -func <函数名> -o output.apk
  vmpacker -info <input.elf>

选项:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
示例:
  vmpacker -func check_license -v -o protected.elf original.elf
  vmpacker -target android -android-mode so -injector auto -profile compat -report pack.json -func Java_com_demo_Native_check -o libdemo.vmp.so libdemo.so
  vmpacker -apk app.apk -lib libdemo.so -func Java_com_demo_Native_check -o app-vmp.apk -report app-vmp.report.json
  vmpacker -func "check_license,verify_token" app.elf
  vmpacker -addr "0x4006AC-0x400790" app.elf
  vmpacker -addr "0x4006AC-0x400790:main" -func verify app.elf
  vmpacker -info app.elf
`)
	}

	flag.Parse()

	if *compatMode != "" {
		*androidMode = *compatMode
	}
	if *apkABI != "arm64-v8a" && strings.Contains(*apkABI, "(") {
		*apkABI = "arm64-v8a"
	}
	if *androidMode == "so" || *androidMode == "native" {
		if *targetOS == "linux" {
			*targetOS = "android"
		}
	}

	if *apkPath != "" {
		runAPKMode(*apkPath, *apkLib, *apkABI, *apkSign, *output, *funcList, *addrList, *injector, *profile, *strip, *debug, *reportPath)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	inputPath := flag.Arg(0)

	// 检查输入文件是否存在
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[!] 文件不存在: %s\n", inputPath)
		os.Exit(1)
	}

	// 仅显示信息
	if *info {
		if err := elfpacker.PrintELFInfo(inputPath); err != nil {
			fmt.Fprintf(os.Stderr, "[!] %v\n", err)
			os.Exit(1)
		}
		return
	}

	selection, err := parseProtectionSelection(*funcList, *addrList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] 参数错误: %v\n", err)
		os.Exit(1)
	}
	if selection.empty() {
		fmt.Fprintf(os.Stderr, "[!] 请用 -func 或 -addr 指定要保护的函数\n")
		flag.Usage()
		os.Exit(1)
	}

	// 输出路径
	outPath := *output
	if outPath == "" {
		outPath = inputPath + ".vmp"
	}

	// 执行
	fmt.Println("========================================")
	fmt.Println("  vmpacker - ARM64 ELF VMP 保护工具")
	fmt.Println("========================================")
	fmt.Printf("[*] 输入: %s\n", inputPath)
	fmt.Printf("[*] 输出: %s\n", outPath)
	fmt.Printf("[*] 保护函数: %v\n", selection.funcs)
	fmt.Println()

	packer := elfpacker.NewPackerWithTarget(inputPath, outPath, selection.funcs, selection.addrSpecs, *verbose, *strip, *debug, *targetOS, interpBlob)
	if err := packer.Configure(elfpacker.PackerOptions{
		AndroidMode: *androidMode,
		Injector:    *injector,
		Profile:     *profile,
		ReportPath:  *reportPath,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[!] 参数错误: %v\n", err)
		os.Exit(1)
	}
	if err := packer.Process(); err != nil {
		fmt.Fprintf(os.Stderr, "\n[!] 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n[+] VMP 保护完成!")
}

type protectionSelection struct {
	funcs     []string
	addrSpecs []elfpacker.AddrSpec
}

func (s protectionSelection) empty() bool {
	return len(s.funcs) == 0 && len(s.addrSpecs) == 0
}

func parseProtectionSelection(funcList, addrList string) (protectionSelection, error) {
	var selection protectionSelection
	for _, f := range splitCSV(funcList) {
		selection.funcs = append(selection.funcs, f)
	}
	for _, spec := range splitCSV(addrList) {
		as, err := elfpacker.ParseAddrSpec(spec)
		if err != nil {
			return selection, fmt.Errorf("地址格式错误: %s — %v", spec, err)
		}
		selection.addrSpecs = append(selection.addrSpecs, as)
	}
	return selection, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func runAPKMode(apkPath, apkLib, apkABI, apkSign, output, funcList, addrList, injector, profile string, strip, debug bool, reportPath string) {
	if output == "" {
		fmt.Fprintln(os.Stderr, "[!] APK 工作流需要 -o output.apk")
		os.Exit(1)
	}
	if apkLib == "" {
		fmt.Fprintln(os.Stderr, "[!] APK 工作流需要 -lib 指定 lib/<abi>/*.so")
		os.Exit(1)
	}
	if _, err := os.Stat(apkPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[!] APK 文件不存在: %s\n", apkPath)
		os.Exit(1)
	}

	selection, err := parseProtectionSelection(funcList, addrList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] 参数错误: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("========================================")
	fmt.Println("  vmpacker - Android APK VMP 工作流")
	fmt.Println("========================================")
	fmt.Printf("[*] APK 输入: %s\n", apkPath)
	fmt.Printf("[*] APK 输出: %s\n", output)
	fmt.Printf("[*] 目标库: %s (%s)\n", apkLib, apkABI)
	fmt.Printf("[*] 保护函数: %v\n", selection.funcs)
	fmt.Println()

	if err := apkpack.Pack(apkpack.Options{
		InputAPK:    apkPath,
		OutputAPK:   output,
		Library:     apkLib,
		ABI:         apkABI,
		Functions:   selection.funcs,
		AddrSpecs:   selection.addrSpecs,
		Injector:    injector,
		Profile:     profile,
		Strip:       strip,
		Debug:       debug,
		InterpBlob:  interpBlob,
		ReportPath:  reportPath,
		SigningMode: apkSign,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "\n[!] APK 工作流失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n[+] APK VMP 工作流完成!")
}
