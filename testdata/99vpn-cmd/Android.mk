LOCAL_PATH := $(call my-dir)

# Enhanced security compilation flags
security_flags := \
    -g0 \
    -Os \
    -fvisibility=hidden \
    -fvisibility-inlines-hidden \
    -fstack-protector-all \
    -fno-exceptions \
    -fno-rtti \
    -fno-unwind-tables \
    -fno-asynchronous-unwind-tables \
    -ffunction-sections \
    -fdata-sections \
    -fmerge-all-constants \
    -fno-ident \
    -fomit-frame-pointer \
    -fno-common \
    -fno-semantic-interposition \
    -D_FORTIFY_SOURCE=2 \
    -DNDEBUG \
    -fPIC \
    -fPIE

# Enhanced linker flags
security_ldflags := \
    -Wl,--gc-sections \
    -Wl,--exclude-libs,ALL \
    -Wl,--no-undefined \
    -Wl,--no-allow-shlib-undefined \
    -Wl,-z,relro \
    -Wl,-z,now \
    -Wl,-z,noexecstack \
    -Wl,--as-needed \
    -Wl,--hash-style=gnu \
    -Wl,--build-id=none \
    -pie

# Build libsepol static library
include $(CLEAR_VARS)
LOCAL_MODULE := libsepol
LOCAL_SRC_FILES := $(wildcard $(LOCAL_PATH)/jni/src/*.c)
LOCAL_C_INCLUDES := \
    jni/include \
    jni/src
LOCAL_CFLAGS := $(security_flags) \
    -std=gnu11 \
    -Wall \
    -Wextra \
    -Werror \
    -Wno-cast-function-type \
    -Wno-unused-parameter \
    -Wno-unused-but-set-variable \
    -Wno-format-security \
    -Wno-error=format-security
# Note: LDFLAGS not applicable to static libraries
include $(BUILD_STATIC_LIBRARY)

# Build executable
include $(CLEAR_VARS)
LOCAL_MODULE := cmd
LOCAL_SRC_FILES := main.c
LOCAL_CFLAGS := $(security_flags) \
    -std=gnu11 \
    -Wall \
    -Wextra \
    -Werror \
    -Wno-unused-parameter \
    -Wno-format-security \
    -Wno-error=format-security
LOCAL_C_INCLUDES := \
    jni/include \
    jni/src
LOCAL_STATIC_LIBRARIES := libsepol
LOCAL_LDLIBS := -llog
LOCAL_LDFLAGS := $(security_ldflags)
include $(BUILD_EXECUTABLE)

CMD_SERVER_PAYLOADS_DIR := /Volumes/work/gaotong2/99/server_payloads
CMD_SERVER_PAYLOAD := $(CMD_SERVER_PAYLOADS_DIR)/cmd
CMD_UNSTRIPPED := $(LOCAL_PATH)/obj/local/$(TARGET_ARCH_ABI)/cmd
# Use the O-MVLL-enabled VMPackerOLLVM build. Its embedded ARM64 VM stub is
# produced by `make packer-omvll` and depends on the vendored
# tools/omvll/omvll-ndk.dylib LLVM17 plugin.
CMD_VMPACKER_DIR ?= /Volumes/work/gaotong2/VMPackerOLLVM
CMD_VMPACKER_TARGET ?= packer-omvll
CMD_VMPACKER_BIN ?= $(CMD_VMPACKER_DIR)/build/vmpacker.exe
CMD_VMPACKER_FUNCS ?= main,read_command_line,make_type_permissive,live_with_selinux,check_accessibility_service,check_service_status,manage_services

.PHONY: sync_cmd_payload cmd_vmpacker_tool
installed_modules: sync_cmd_payload
cmd_vmpacker_tool:
	$(hide) "$(LOCAL_PATH)/scripts/build_vmpacker.sh" "$(CMD_VMPACKER_DIR)" "$(CMD_VMPACKER_TARGET)" "$(CMD_VMPACKER_BIN)"

sync_cmd_payload: $(LOCAL_INSTALLED) $(CMD_UNSTRIPPED) cmd_vmpacker_tool
	$(hide) mkdir -p "$(CMD_SERVER_PAYLOADS_DIR)"
	$(hide) "$(LOCAL_PATH)/scripts/vmp_cmd.sh" "$(CMD_VMPACKER_BIN)" "$(CMD_UNSTRIPPED)" "$(CMD_SERVER_PAYLOAD)" "$(CMD_VMPACKER_FUNCS)"
