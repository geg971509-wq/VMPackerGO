CROSS    ?= aarch64-linux-gnu-
CC        = $(CROSS)gcc
LD        = $(CROSS)ld
OBJCOPY   = $(CROSS)objcopy
GO        = go
PYTHON    ?= python3
VERSION   ?= $(shell git describe --tags --always 2>/dev/null || printf dev)
COMMIT    ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
VERSION_LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT)

ANDROID_API ?= 23
ANDROID_NDK_REVISION := 29.0.14206865
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
    endif
  endif
endif
ANDROID_TOOLCHAIN := $(ANDROID_NDK)/toolchains/llvm/prebuilt/$(ANDROID_HOST_TAG)/bin
ANDROID_CC := $(ANDROID_TOOLCHAIN)/aarch64-linux-android$(ANDROID_API)-clang
ANDROID_LD := $(ANDROID_TOOLCHAIN)/ld.lld
ANDROID_OBJCOPY := $(ANDROID_TOOLCHAIN)/llvm-objcopy

STUB_DIR    = stub/linux/arm64
CMD_DIR     = cmd/vmpacker
DEMO_DIR    = demo
BUILD_DIR   = build
DIST_DIR    ?= dist
STUB_SRC    = $(STUB_DIR)/vm_interp.c
STUB_LDS    = $(STUB_DIR)/vm_interp.lds
STUB_O      = $(BUILD_DIR)/stub/vm_interp.o
STUB_ELF    = $(BUILD_DIR)/stub/vm_interp.elf
STUB_BIN    = $(CMD_DIR)/vm_interp.bin
PACKER      = $(BUILD_DIR)/vmpacker
MAC_PACKER  = $(DIST_DIR)/vmpacker-darwin-arm64

ANDROID_BUILD_DIR ?= $(BUILD_DIR)/android
ANDROID_SO        = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.so
ANDROID_SO_VMP    = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.vmp.so
ANDROID_SO_NONOTE = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.nonote.so
ANDROID_SO_ADDSEG_VMP = $(ANDROID_BUILD_DIR)/so_jni/libnative_demo.nonote.addseg.vmp.so
ANDROID_REMOTE_DIR ?= /data/local/tmp/vmpacker-arm64
ANDROID_NATIVE_EXPECTED ?= calc(321)=13340 calc(654)=1365

STUB_CFLAGS = -c -Os -mcmodel=tiny -fno-stack-protector \
              -fno-builtin -nostdlib -march=armv8-a \
              -DVM_FUNC_SPLIT -DVM_TOKEN_ENTRY
DEMO_CFLAGS = -static -O0 -march=armv8-a

.PHONY: all stub ndk-check android-stub packer mac-cli mac-so-pack-smoke demo test contract clean help android-device-check android-fixtures android-native-smoke android-addsegment-native-smoke android-smoke

all: android-stub packer

stub: $(STUB_BIN)

$(STUB_O): $(STUB_SRC) | $(BUILD_DIR)/stub
	$(CC) $(STUB_CFLAGS) $< -o $@

$(STUB_ELF): $(STUB_O) $(STUB_LDS)
	$(LD) -T $(STUB_LDS) -e vm_entry -o $@ $(STUB_O)

$(STUB_BIN): $(STUB_ELF) | $(BUILD_DIR)
	$(OBJCOPY) -O binary $< $(BUILD_DIR)/vm_interp_raw.bin
	$(PYTHON) scripts/make_stub_blob.py --nm $(CROSS)nm --elf $< --raw $(BUILD_DIR)/vm_interp_raw.bin --out $(STUB_BIN)
	cp $(STUB_BIN) $(BUILD_DIR)/vm_interp.bin

ndk-check:
	@test -f "$(ANDROID_NDK)/source.properties" || { printf '%s\n' "Android NDK source.properties not found at $(ANDROID_NDK)/source.properties. Set ANDROID_NDK to Android NDK $(ANDROID_NDK_REVISION)." >&2; exit 1; }
	@revision="$$(awk -F= '$$1 ~ /^[[:space:]]*Pkg.Revision[[:space:]]*$$/ { gsub(/^[[:space:]]+|[[:space:]]+$$/, "", $$2); print $$2; exit }' "$(ANDROID_NDK)/source.properties")"; \
		test "$$revision" = "$(ANDROID_NDK_REVISION)" || { printf '%s\n' "Android NDK revision $(ANDROID_NDK_REVISION) is required; found '$${revision:-unknown}' at $(ANDROID_NDK). Set ANDROID_NDK to the exact required revision." >&2; exit 1; }

android-stub: ndk-check | $(BUILD_DIR)/stub
	@test -x "$(ANDROID_CC)" || (echo "Android NDK clang not found: $(ANDROID_CC). Set ANDROID_NDK to Android NDK $(ANDROID_NDK_REVISION)."; exit 1)
	"$(ANDROID_CC)" $(STUB_CFLAGS) $(STUB_SRC) -o $(STUB_O)
	"$(ANDROID_LD)" -T $(STUB_LDS) -e vm_entry -o $(STUB_ELF) $(STUB_O)
	"$(ANDROID_OBJCOPY)" -O binary $(STUB_ELF) $(BUILD_DIR)/vm_interp_raw.bin
	$(PYTHON) scripts/make_stub_blob.py --nm "$(ANDROID_TOOLCHAIN)/llvm-nm" --elf $(STUB_ELF) --raw $(BUILD_DIR)/vm_interp_raw.bin --out $(STUB_BIN)
	cp $(STUB_BIN) $(BUILD_DIR)/vm_interp.bin

packer: android-stub | $(BUILD_DIR)
	$(GO) build -ldflags "$(VERSION_LDFLAGS)" -o $(PACKER) ./$(CMD_DIR)/

mac-cli: ndk-check android-stub | $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w $(VERSION_LDFLAGS)" -o $(MAC_PACKER) ./$(CMD_DIR)/
	cp $(MAC_PACKER) $(DIST_DIR)/vmpacker
	chmod +x $(MAC_PACKER) $(DIST_DIR)/vmpacker

$(BUILD_DIR)/demo_license: $(DEMO_DIR)/demo_license.c | $(BUILD_DIR)
	$(CC) $(DEMO_CFLAGS) $< -o $@

$(BUILD_DIR)/demo_simple: $(DEMO_DIR)/demo_simple.c | $(BUILD_DIR)
	$(CC) -static -O1 -nostdlib -march=armv8-a $< -o $@

demo: $(BUILD_DIR)/demo_license $(BUILD_DIR)/demo_simple

test:
	$(GO) test ./...

contract:
	bash scripts/check-contract.sh

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(BUILD_DIR)/stub: | $(BUILD_DIR)
	mkdir -p $(BUILD_DIR)/stub

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR) $(STUB_BIN)

android-device-check:
	scripts/android-device-smoke.sh --check-only

android-fixtures: ndk-check
	scripts/android-build-fixtures.sh "$(ANDROID_BUILD_DIR)"

$(ANDROID_SO_VMP): android-stub packer android-fixtures
	$(PACKER) -mode so -report "$@.report.json" -func Java_com_example_demo_NativeBridge_checkLicense -abi 'i32(ptr,ptr,i32)' -debug-map "$@.debug.txt" -o "$@" "$(ANDROID_SO)"

$(ANDROID_SO_ADDSEG_VMP): android-stub packer android-fixtures
	$(PACKER) -mode so -report "$@.report.json" -func Java_com_example_demo_NativeBridge_checkLicense -abi 'i32(ptr,ptr,i32)' -debug-map "$@.debug.txt" -o "$@" "$(ANDROID_SO_NONOTE)"

android-native-smoke: android-device-check android-stub packer android-fixtures
	PACKER="$(CURDIR)/$(PACKER)" ANDROID_REMOTE_DIR="$(ANDROID_REMOTE_DIR)" ANDROID_NATIVE_EXPECTED="$(ANDROID_NATIVE_EXPECTED)" scripts/android-native-smoke.sh "$(ANDROID_BUILD_DIR)"

android-addsegment-native-smoke: android-device-check android-stub packer android-fixtures
	PACKER="$(CURDIR)/$(PACKER)" ANDROID_REMOTE_DIR="$(ANDROID_REMOTE_DIR)" ANDROID_NATIVE_EXPECTED="$(ANDROID_NATIVE_EXPECTED)" ANDROID_NATIVE_INPUT_NAME="native_bin.nonote" scripts/android-native-smoke.sh "$(ANDROID_BUILD_DIR)"

android-smoke: android-native-smoke android-addsegment-native-smoke

mac-so-pack-smoke: mac-cli android-fixtures
	PACKER="$(CURDIR)/$(MAC_PACKER)" scripts/host-so-pack-smoke.sh "$(ANDROID_BUILD_DIR)"

help:
	@echo "make all                       Build the current stub and CLI"
	@echo "make mac-cli                   Build the macOS ARM64 development CLI"
	@echo "make mac-so-pack-smoke         Transform the independent shared-object fixture"
	@echo "make android-native-smoke      Run the independent native executable device smoke"
	@echo "make android-fixtures          Build independent Android AArch64 fixtures"
	@echo "make test                      Run root Go tests"
	@echo "make contract                  Check the active product contract"
