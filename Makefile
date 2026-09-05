GO       ?= go
VERSION  ?= $(shell git describe --tags --always 2>/dev/null || printf dev)
COMMIT   ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
LDFLAGS   = -X main.version=$(VERSION) -X main.commit=$(COMMIT)

ANDROID_NDK_REVISION := 29.0.14206865
ANDROID_NDK ?= $(ANDROID_NDK_HOME)
BUILD_DIR  ?= build
DIST_DIR   ?= dist
PACKER      = $(BUILD_DIR)/vmpacker
MAC_PACKER  = $(DIST_DIR)/vmpacker-darwin-arm64

ANDROID_BUILD_DIR ?= $(BUILD_DIR)/android

.PHONY: all packer mac-cli format-check test test-race fuzz-smoke vet contract release-rehearsal release-contract verify demo-cases evidence-self-test ios-dylib-validation \
	ndk-check runtime-integration android-device-check android-fixtures clean help

all: packer

packer: | $(BUILD_DIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(PACKER) ./cmd/vmpacker

mac-cli: | $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath \
		-ldflags "-s -w $(LDFLAGS)" -o $(MAC_PACKER) ./cmd/vmpacker
	cp $(MAC_PACKER) $(DIST_DIR)/vmpacker
	chmod +x $(MAC_PACKER) $(DIST_DIR)/vmpacker

format-check:
	@files="$$(gofmt -l cmd internal)"; \
		test -z "$$files" || { printf '%s\n' "gofmt required:" "$$files" >&2; exit 1; }

test:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

fuzz-smoke:
	GO="$(GO)" python3 scripts/run-host-fuzz-smoke.py

vet:
	$(GO) vet ./...

demo-cases:
	python3 scripts/validate-demo-cases.py

evidence-self-test: demo-cases
	python3 scripts/validate-device-evidence-test.py
	python3 scripts/validate-release-evidence-test.py

contract: evidence-self-test
	bash scripts/check-contract.sh
	bash scripts/check-contract-test.sh

release-rehearsal:
	python3 scripts/rehearse-release-gates.py

release-contract:
	bash scripts/check-contract.sh --release

verify: format-check test test-race fuzz-smoke vet contract release-rehearsal ios-dylib-validation

ios-dylib-validation:
	bash scripts/ios-dylib-validation.sh

ndk-check:
	@test -n "$(ANDROID_NDK)" || { printf '%s\n' "Set ANDROID_NDK to Android NDK $(ANDROID_NDK_REVISION)." >&2; exit 1; }
	@test -f "$(ANDROID_NDK)/source.properties" || { printf '%s\n' "Android NDK source.properties is missing." >&2; exit 1; }
	@revision="$$(awk -F= '$$1 ~ /^[[:space:]]*Pkg.Revision[[:space:]]*$$/ { gsub(/^[[:space:]]+|[[:space:]]+$$/, "", $$2); print $$2; exit }' "$(ANDROID_NDK)/source.properties")"; \
		test "$$revision" = "$(ANDROID_NDK_REVISION)" || { printf '%s\n' "Android NDK revision $(ANDROID_NDK_REVISION) is required; found '$${revision:-unknown}'." >&2; exit 1; }

runtime-integration: ndk-check
	ANDROID_NDK="$(ANDROID_NDK)" $(GO) test -count=1 -run TestBuildInstalledExactR29Object ./internal/runtime

android-device-check:
	scripts/android-device-check.sh --attest-physical

android-fixtures: ndk-check
	scripts/android-build-fixtures.sh "$(ANDROID_BUILD_DIR)"

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

help:
	@printf '%s\n' \
		"make packer               Build the development CLI" \
		"make mac-cli              Build the macOS ARM64 CLI" \
		"make format-check         Require canonical gofmt formatting for active Go source" \
		"make verify               Run format, Go, race, fuzz, vet, contract, and rehearsal gates" \
		"make fuzz-smoke           Run bounded mutation fuzzing for host decoder/parsers" \
		"make contract             Run product/evidence contract self-tests" \
		"make release-rehearsal    Replay local release gates and prove missing external evidence fails closed" \
		"make release-contract     Validate final external release evidence" \
		"make ios-dylib-validation Build and pack a real arm64 iPhoneOS MH_DYLIB on macOS" \
		"make demo-cases            Validate the exact 85-demo device case specification" \
		"make evidence-self-test    Test device/release evidence validators" \
		"make runtime-integration  Build and validate the runtime with exact NDK r29" \
		"make android-device-check Attest the connected physical Android device" \
		"make android-fixtures     Cross-compile Android ELF fixtures"
