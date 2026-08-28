# VMPacker

VMPacker 是一个处于开发阶段的虚拟机保护工具，用于处理独立的 Android AArch64 ELF64 二进制文件。官方产品形态是 macOS ARM64 命令行程序。

## 产品范围

- 主机产品：macOS ARM64 CLI。
- 输入：独立的 Android AArch64 ELF64 动态库（`.so`）以及 PIE/`ET_EXEC` 原生可执行文件。
- 最低 Android 运行版本：API 23。
- 保护目标选择：使用 `-abi` 直接选择一个函数名或地址，也可使用 manifest v1 携带显式 ABI 批量选择。
- 输出：转换后的 ELF，以及可选的 report v1 JSON 和显式 debug map。report v1 由当前 Phase 2 CLI 实现。
- 非活动产品范围：APK、AAB、GUI、Linux 发行版和 Windows 发行版。历史 APK 与 GUI 工作保留在 `archive/` 下，不受支持。

VMPacker 只能提高逆向分析成本，不能让代码变得无法检查、复制、修改或绕过。

## 开发状态

本仓库仍处于开发阶段，不是可发布产品。发布门槛包括 API 23+ 兼容性、4 KiB 与 16 KiB 页面验证、BTI/PAC 行为、受支持指令的正确性、ELF 加载器兼容性以及真机 smoke 覆盖。在相关检查建立并通过前，不得声称仓库已经通过这些门槛。

请参阅[产品契约](docs/product-contract.md)、[开发指南](docs/development.md)和[报告格式](docs/report-schema-v1.md)。

## 开发命令

```sh
make android-stub ANDROID_NDK=/path/to/android-ndk-r29
make packer ANDROID_NDK=/path/to/android-ndk-r29
go list ./...
go test ./...
go vet ./cmd/vmpacker ./internal/...
bash scripts/check-contract.sh

./build/vmpacker -mode so -func exported_name -abi 'i32(ptr)' \
  -report pack.json -o libdemo.vmp.so libdemo.so
```

当前 CLI 构建仍使用固定解释器 blob 路径，该路径将在后续阶段替换。详情见 [development.md](docs/development.md)。

## 许可证与使用

Copyright 2026 LeoChen。

VMPacker 仅按 GNU Affero General Public License version 3（`AGPL-3.0-only`）授权。请参阅 [LICENSE](LICENSE) 和 [NOTICE](NOTICE)。

请仅处理您拥有或获准修改的二进制文件，并遵守适用法律。此说明仅供参考，不在 AGPL-3.0-only 之外增加限制。
