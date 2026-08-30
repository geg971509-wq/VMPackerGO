# ============================================================
# VMP 工具链 Makefile
# make all    → 编译 C stub → 嵌入 Go → 输出到 build/
# make stub   → 仅编译 VM 解释器 blob
# make packer → 仅编译 Go packer（需先 make stub）
# make demo   → 交叉编译 demo 程序
# make test   → 运行 Go 单元测试
# make clean  → 清理所有产物
# ============================================================

# 交叉编译工具链
CROSS   ?= aarch64-linux-gnu-
CC       = $(CROSS)gcc
LD       = $(CROSS)ld
OBJCOPY  = $(CROSS)objcopy
GO       = go
PYTHON   ?= python3

# Android NDK toolchain (for APK/JNI arm64-v8a targets)
ANDROID_API ?= 23
ANDROID_NDK ?= $(ANDROID_NDK_HOME)
ifeq ($(ANDROID_NDK),)
ANDROID_NDK := $(NDK_HOME)
endif
ifeq ($(ANDROID_NDK),)
ANDROID_NDK := $(HOME)/Library/Android/sdk/ndk/current
endif
ifeq ($(wildcard $(ANDROID_NDK)/toolchains/llvm/prebuilt),)
ANDROID_NDK := $(firstword $(wildcard /opt/homebrew/Caskroom/android-ndk/*/AndroidNDK*.app/Contents/NDK) $(wildcard /usr/local/Caskroom/android-ndk/*/AndroidNDK*.app/Contents/NDK) $(ANDROID_NDK))
endif
ANDROID_HOST_TAG ?= darwin-x86_64
ifeq ($(shell uname -s),Darwin)
  ifeq ($(shell uname -m),arm64)
    ifneq ($(wildcard $(ANDROID_NDK)/toolchains/llvm/prebuilt/darwin-arm64),)
      ANDROID_HOST_TAG := darwin-arm64
    else
      ANDROID_HOST_TAG := darwin-x86_64
    endif
  endif
endif
ANDROID_TOOLCHAIN := $(ANDROID_NDK)/toolchains/llvm/prebuilt/$(ANDROID_HOST_TAG)/bin
ANDROID_CC := $(ANDROID_TOOLCHAIN)/aarch64-linux-android$(ANDROID_API)-clang
ANDROID_LD := $(ANDROID_TOOLCHAIN)/ld.lld
ANDROID_OBJCOPY := $(ANDROID_TOOLCHAIN)/llvm-objcopy

# 目录
STUB_DIR   = stub/linux/arm64
CMD_DIR    = cmd/vmpacker
DEMO_DIR   = demo
BUILD_DIR  = build
DIST_DIR   ?= dist
HOST_GOOS  ?= $(shell $(GO) env GOOS)
HOST_GOARCH ?= $(shell $(GO) env GOARCH)

# ------ VM 解释器 blob ------
STUB_SRC   = $(STUB_DIR)/vm_interp.c
STUB_LDS   = $(STUB_DIR)/vm_interp.lds
STUB_O     = $(BUILD_DIR)/stub/vm_interp.o
STUB_ELF   = $(BUILD_DIR)/stub/vm_interp.elf
STUB_BIN   = $(CMD_DIR)/vm_interp.bin

# ------ Go packer ------
PACKER     = $(BUILD_DIR)/vmpacker.exe
HOST_PACKER = $(DIST_DIR)/vmpacker-$(HOST_GOOS)-$(HOST_GOARCH)

# ------ Demo ------
DEMO_LICENSE     = $(BUILD_DIR)/demo_license
DEMO_SIMPLE      = $(BUILD_DIR)/demo_simple

# ------ Android smoke fixtures ------
ANDROID_BUILD_DIR ?= $(BUILD_DIR)/android
ANDROID_SO        = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.so
ANDROID_SO_VMP    = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.vmp.so
ANDROID_SO_NONOTE = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.nonote.so
ANDROID_SO_ADDSEG_VMP = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.nonote.addseg.vmp.so
ANDROID_APK_SMOKE_DIR ?= $(ANDROID_BUILD_DIR)/apk-smoke
ANDROID_ADDSEG_APK_SMOKE_DIR ?= $(ANDROID_BUILD_DIR)/apk-addsegment-smoke
ANDROID_APK_WORKFLOW_DIR ?= $(ANDROID_BUILD_DIR)/apk-workflow
ANDROID_REMOTE_DIR ?= /data/local/tmp/vmpacker-arm64
APK_SMOKE_EXPECTED_LOG ?= check(1234)=29711 check(1111)=19398
ANDROID_NATIVE_EXPECTED ?= calc(321)=13340 calc(654)=1365

# 编译选项 (必须 -mcmodel=tiny，禁止 -fPIC)
STUB_CFLAGS = -c -Os -mcmodel=tiny -fno-stack-protector \
              -fno-builtin -nostdlib -march=armv8-a \
              -DVM_INDIRECT_DISPATCH -DVM_FUNC_SPLIT -DVM_TOKEN_ENTRY

DEMO_CFLAGS = -static -O0 -march=armv8-a

# ============================================================
.PHONY: all stub android-stub packer mac-cli mac-so-pack-smoke demo test clean help gui sync-public android-device-check android-fixtures android-apk-smoke android-native-smoke android-smoke android-addsegment-apk-smoke android-addsegment-native-smoke android-addsegment-smoke android-apk-workflow-smoke

all: stub packer
	@echo ""
	@echo "[+] Build complete: $(BUILD_DIR)/"

# ------ VM 解释器 blob ------
stub: $(STUB_BIN)

$(STUB_O): $(STUB_SRC) | $(BUILD_DIR)/stub
	$(CC) $(STUB_CFLAGS) $< -o $@

$(STUB_ELF): $(STUB_O) $(STUB_LDS)
	$(LD) -T $(STUB_LDS) -e vm_entry -o $@ $(STUB_O)

$(STUB_BIN): $(STUB_ELF) | $(BUILD_DIR)
	$(OBJCOPY) -O binary $< $(BUILD_DIR)/vm_interp_raw.bin
	@$(PYTHON) scripts/make_stub_blob.py --nm $(CROSS)nm --elf $< --raw $(BUILD_DIR)/vm_interp_raw.bin --out $(STUB_BIN)
	@cp $(STUB_BIN) $(BUILD_DIR)/vm_interp.bin

android-stub: | $(BUILD_DIR)/stub
	@test -x "$(ANDROID_CC)" || (echo "[!] Android NDK clang not found: $(ANDROID_CC)"; echo "    Install Android Studio/NDK or set ANDROID_NDK=/path/to/ndk"; exit 1)
	"$(ANDROID_CC)" $(STUB_CFLAGS) $(STUB_SRC) -o $(STUB_O)
	"$(ANDROID_LD)" -T $(STUB_LDS) -e vm_entry -o $(STUB_ELF) $(STUB_O)
	"$(ANDROID_OBJCOPY)" -O binary $(STUB_ELF) $(BUILD_DIR)/vm_interp_raw.bin
	@$(PYTHON) scripts/make_stub_blob.py --nm "$(ANDROID_TOOLCHAIN)/llvm-nm" --elf $(STUB_ELF) --raw $(BUILD_DIR)/vm_interp_raw.bin --out $(STUB_BIN)
	@cp $(STUB_BIN) $(BUILD_DIR)/vm_interp.bin

# ------ Go packer (embed vm_interp.bin) ------
packer: $(STUB_BIN) | $(BUILD_DIR)
	@rm -f $(PACKER)
	$(GO) build -o $(PACKER) ./$(CMD_DIR)/
	@echo "[+] packer: $(PACKER)"

mac-cli: android-stub | $(DIST_DIR)
	CGO_ENABLED=0 GOOS=$(HOST_GOOS) GOARCH=$(HOST_GOARCH) $(GO) build -trimpath -ldflags "-s -w" -o $(HOST_PACKER) ./$(CMD_DIR)/
	@cp $(HOST_PACKER) $(DIST_DIR)/vmpacker
	@chmod +x $(HOST_PACKER) $(DIST_DIR)/vmpacker
	@echo "[+] standalone CLI: $(HOST_PACKER)"
	@echo "[+] direct runner:   $(DIST_DIR)/vmpacker"

# ------ Demo 程序 ------
demo: $(DEMO_LICENSE) $(DEMO_SIMPLE)

$(DEMO_LICENSE): $(DEMO_DIR)/demo_license.c | $(BUILD_DIR)
	$(CC) $(DEMO_CFLAGS) $< -o $@
	@echo "[+] demo: $@"

$(DEMO_SIMPLE): $(DEMO_DIR)/demo_simple.c | $(BUILD_DIR)
	$(CC) -static -O1 -nostdlib -march=armv8-a $< -o $@
	@echo "[+] demo: $@"

# ------ 测试 ------
test:
	$(GO) test ./...

# ------ 目录创建 ------
$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

$(BUILD_DIR)/stub: | $(BUILD_DIR)
	@mkdir -p $(BUILD_DIR)/stub

$(DIST_DIR):
	@mkdir -p $(DIST_DIR)

# ------ 清理 ------
clean:
	@rm -rf $(BUILD_DIR) $(DIST_DIR) $(STUB_BIN)
	@echo "[+] cleaned"

android-device-check:
	@scripts/android-device-smoke.sh --check-only

android-fixtures:
	@scripts/android-build-fixtures.sh "$(ANDROID_BUILD_DIR)"

$(ANDROID_SO_VMP): android-stub packer android-fixtures
	$(PACKER) -target android -android-mode so -injector auto -profile compat -report "$@.report.json" -func Java_com_example_demo_NativeBridge_checkLicense -debug -o "$@" "$(ANDROID_SO)"

$(ANDROID_SO_ADDSEG_VMP): android-stub packer android-fixtures
	$(PACKER) -target android -android-mode so -injector add-segment -profile compat -report "$@.report.json" -func Java_com_example_demo_NativeBridge_checkLicense -debug -o "$@" "$(ANDROID_SO_NONOTE)"

android-apk-smoke: android-device-check $(ANDROID_SO_VMP)
	@APK_SMOKE_EXPECTED_LOG="$(APK_SMOKE_EXPECTED_LOG)" \
	  scripts/android-build-smoke-apk.sh "$(ANDROID_SO_VMP)" "$(ANDROID_APK_SMOKE_DIR)"

android-addsegment-apk-smoke: android-device-check $(ANDROID_SO_ADDSEG_VMP)
	@APK_SMOKE_EXPECTED_LOG="$(APK_SMOKE_EXPECTED_LOG)" \
	  scripts/android-build-smoke-apk.sh "$(ANDROID_SO_ADDSEG_VMP)" "$(ANDROID_ADDSEG_APK_SMOKE_DIR)"

android-native-smoke: android-device-check android-stub packer android-fixtures
	@PACKER="$(CURDIR)/$(PACKER)" \
	  ANDROID_REMOTE_DIR="$(ANDROID_REMOTE_DIR)" \
	  ANDROID_NATIVE_EXPECTED="$(ANDROID_NATIVE_EXPECTED)" \
	  scripts/android-native-smoke.sh "$(ANDROID_BUILD_DIR)"

android-addsegment-native-smoke: android-device-check android-stub packer android-fixtures
	@PACKER="$(CURDIR)/$(PACKER)" \
	  ANDROID_REMOTE_DIR="$(ANDROID_REMOTE_DIR)" \
	  ANDROID_NATIVE_EXPECTED="$(ANDROID_NATIVE_EXPECTED)" \
	  ANDROID_NATIVE_INPUT_NAME="native_bin.nonote" \
	  ANDROID_NATIVE_INJECTOR="add-segment" \
	  scripts/android-native-smoke.sh "$(ANDROID_BUILD_DIR)"

android-smoke: android-apk-smoke android-native-smoke
	@echo "[+] Android APK + native smoke passed"

android-addsegment-smoke: android-addsegment-apk-smoke android-addsegment-native-smoke
	@echo "[+] Android add-segment APK + native smoke passed"

android-apk-workflow-smoke: android-device-check android-stub packer android-fixtures
	@PACKER="$(CURDIR)/$(PACKER)" \
	  APK_WORKFLOW_DIR="$(CURDIR)/$(ANDROID_APK_WORKFLOW_DIR)" \
	  APK_SMOKE_EXPECTED_LOG="$(APK_SMOKE_EXPECTED_LOG)" \
	  scripts/android-apk-workflow-smoke.sh "$(ANDROID_BUILD_DIR)"

mac-so-pack-smoke: mac-cli android-fixtures
	@PACKER="$(CURDIR)/$(HOST_PACKER)" \
	  scripts/host-so-pack-smoke.sh "$(ANDROID_BUILD_DIR)"

# ------ 帮助 ------
help:
	@echo "make all     - 编译 stub + packer (输出到 build/)"
	@echo "make stub    - 仅编译 VM 解释器 blob"
	@echo "make android-stub - 使用 Android NDK 编译 APK/JNI arm64-v8a stub blob"
	@echo "make packer  - 编译 Go packer (自动嵌入 blob)"
	@echo "make mac-cli - 构建 macOS/当前主机可直接运行的 dist/vmpacker"
	@echo "make mac-so-pack-smoke - 用 dist/vmpacker 在主机侧直接 pack .so"
	@echo "make android-device-check - 检查 USB Android arm64/su 测试环境"
	@echo "make android-fixtures - 构建 Android arm64 smoke fixtures"
	@echo "make android-apk-smoke - pack JNI .so 并构建/安装最小 APK 验证"
	@echo "make android-native-smoke - pack Android native executable 并真机验证"
	@echo "make android-smoke - 运行 APK + native 全量 Android smoke"
	@echo "make android-addsegment-smoke - 使用无 PT_NOTE fixtures 验证 add-segment 注入器"
	@echo "make android-apk-workflow-smoke - 输入 APK，pack so，重打包/签名/安装 smoke"
	@echo "make gui     - 编译 GUI 版本 + NSIS 安装包"
	@echo "make demo    - 交叉编译 demo 程序"
	@echo "make test    - 运行单元测试"
	@echo "make clean        - 清理所有产物"
	@echo "make sync-public  - 同步到公开仓库 (vmpack remote)"

# ------ GUI 版本 (Wails + NSIS) ------
GUI_DIR = vmp-gui

gui: stub
	@copy /Y "$(subst /,\,$(STUB_BIN))" "$(subst /,\,$(GUI_DIR))\backend\api\vm_interp.bin" > nul
	@powershell -Command "$$env:PATH = 'C:\Program Files (x86)\NSIS;' + $$env:PATH; cd '$(GUI_DIR)'; wails build -nsis"
	@echo "[+] GUI installer: $(GUI_DIR)/build/bin/"

# ------ 同步公开仓库 ------
sync-public:
	@powershell -ExecutionPolicy Bypass -File sync-public.ps1
