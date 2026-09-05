APP_ABI := arm64-v8a
APP_PLATFORM := android-34
APP_STL := none
APP_OPTIM := release
APP_STRIP_MODE := none
APP_THIN_ARCHIVE := true
APP_PIE := true

# Disable debug features
APP_DEBUG := false
APP_DEBUGGABLE := false

# Additional compiler flags
APP_CFLAGS := -fvisibility=hidden -fvisibility-inlines-hidden
APP_CPPFLAGS := -fvisibility=hidden -fvisibility-inlines-hidden
APP_LDFLAGS := -Wl,--build-id=none -Wl,--exclude-libs,ALL
