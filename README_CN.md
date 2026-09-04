# VMPacker

VMPacker 是一个处于开发阶段的虚拟机保护工具，用于处理独立的 Android AArch64 ELF64 二进制文件。官方产品形态是 macOS ARM64 命令行程序。

## 产品范围

- 主机产品：macOS ARM64 CLI。
- 输入：独立的 Android AArch64 ELF64 动态库（`.so`）以及 PIE/`ET_EXEC` 原生可执行文件。
- 最低 Android 运行版本：API 23。
- 保护目标选择：使用 `-abi` 直接选择一个函数名或地址，也可使用 manifest v1 携带显式 ABI 批量选择。
- 目标输出：转换后的 ELF，以及可选的 report v1 JSON 和显式 debug map。
- 非活动产品范围：APK、AAB、GUI、Linux 发行版和 Windows 发行版。历史 APK 与 GUI 工作保留在 `archive/` 下，不受支持。

VMPacker 只能提高逆向分析成本，不能让代码变得无法检查、复制、修改或绕过。

## 开发状态

宿主侧产品化链路已经实现，并由仓库 Verification 工作流覆盖：fail-closed 运行时错误语义、带 guard 的受限运行时资源、显式 ARM64 能力策略、准确 NDK r29 runtime 构建、plan-first ELF 重写、受限的近/远入口跳转、结构化 C++ exception/unwind bridge、精确 85-demo 设备 case 规格、fuzz/资源预算门，以及证据驱动的发行工具。

项目**仍然不是 release-ready**。正式发行仍需要真实物理 Android 设备上的 API/页面大小/BTI/PAC/ASLR/CPU 特征矩阵证据，85 个 demo 的 baseline-versus-packed 对比执行，原子竞争与 C++ exception/unwind 真机证据，Developer ID 签名、Apple 公证，以及独立发行审核。这些外部事实不会由宿主测试推断，也不会由构建脚本伪造。

请参阅[产品契约](docs/product-contract.md)、[当前支持矩阵](docs/support-matrix.md)、[设备证据格式](docs/device-evidence-schema-v1.md)、[发行流程](docs/release-process.md)、[修复审计](docs/remediation-audit-20260903.md)和[报告格式](docs/report-schema-v1.md)。

## 开发命令

```sh
./build.sh
make packer
make verify
make demo-cases
make evidence-self-test
make runtime-integration ANDROID_NDK=/path/to/android-ndk-r29

./build/vmpacker -ndk /path/to/android-ndk-r29 -mode so \
  -func exported_name -abi 'i32(ptr)' -report pack.json \
  -o libdemo.vmp.so libdemo.so
```

根目录 `build.sh` 会把当前 Git checkout 构建成 macOS ARM64 可执行文件，验证 Mach-O 架构，并生成 `dist/vmpacker-darwin-arm64` 及内容相同的直接运行文件 `dist/vmpacker`。

manifest v1 输入必须是本地普通文件，大小不得超过 16 MiB，并且一次最多选择 4096 个函数。文件类型和大小会在 JSON 解析前检查，避免异常文件或特殊文件把 manifest 解析变成无界读取。

每次打包都会创建本次运行专用的 opcode map，翻译选中的函数，使用准确 Android NDK `29.0.14206865` 从内嵌源码重新构建并验证 AArch64 可重定位 runtime，生成不可变 rewrite plan，再把计划应用到新的内存 ELF 映像并在发布前重新解析。运行时使用显式 fail-closed fault、独立映射且带 guard 的 shadow stack，以及动态受限的 protected-call frame。rewrite plan 覆盖 0x4000 对齐且遵守 W^X 的 runtime load、runtime relocation、加密字节码/token 描述符、BTI 感知入口 patch、直接 `B` 无法到达时的内联 `ADRP+ADD+BR` long-entry veneer、program-header 变更，以及受支持的 GNU unwind index 集成。通用 native external tail branch 不做 call+return 近似，而是确定性拒绝。

`scripts/` 下的物理设备工具负责设备资格检查、精确 85-demo differential matrix、专项语义 fixture、证据合并以及按准确 commit/manifest 校验证据。发行工具只在设备证据通过后处理带 tag 的 macOS ARM64 候选制品、Developer ID 签名、公证、源码/校验和/证据文件；最终发行契约还会重建该 tag 对应的 Git archive，并拒绝与准确 tag 不一致的源码包。独立审核仍是单独的强制门。

## 许可证与使用

Copyright 2026 LeoChen。

VMPacker 仅按 GNU Affero General Public License version 3（`AGPL-3.0-only`）授权。请参阅 [LICENSE](LICENSE) 和 [NOTICE](NOTICE)。

请仅处理您拥有或获准修改的二进制文件，并遵守适用法律。此说明仅供参考，不在 AGPL-3.0-only 之外增加限制。
