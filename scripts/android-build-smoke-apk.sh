#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  cat >&2 <<'USAGE'
Usage: scripts/android-build-smoke-apk.sh libnative_demo.vmp.so output-dir

Builds, signs, installs, and launches a minimal APK that loads a packed
arm64-v8a .so and calls a generated Java native bridge. This is a local
authorized smoke test for APK app-UID loading.

Environment overrides:
  APK_SMOKE_PACKAGE             default: com.vmpacker.smoke
  APK_SMOKE_ACTIVITY_CLASS      default: SmokeActivity
  APK_SMOKE_NATIVE_PACKAGE      default: com.example.demo
  APK_SMOKE_NATIVE_CLASS        default: NativeBridge
  APK_SMOKE_LIBRARY_NAME        default: native_demo
  APK_SMOKE_JNI_METHOD          default: checkLicense
  APK_SMOKE_VALUES              default: "1234 1111"
  APK_SMOKE_EXPECTED_LOG        default: "check(1234)=29711 check(1111)=19398"
USAGE
  exit 2
fi

LIB_SO="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
OUT_DIR="$2"
mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
PKG="${APK_SMOKE_PACKAGE:-com.vmpacker.smoke}"
ACTIVITY_CLASS="${APK_SMOKE_ACTIVITY_CLASS:-SmokeActivity}"
ACTIVITY="${PKG}.${ACTIVITY_CLASS}"
NATIVE_PKG="${APK_SMOKE_NATIVE_PACKAGE:-com.example.demo}"
NATIVE_CLASS="${APK_SMOKE_NATIVE_CLASS:-NativeBridge}"
LIBRARY_NAME="${APK_SMOKE_LIBRARY_NAME:-native_demo}"
JNI_METHOD="${APK_SMOKE_JNI_METHOD:-checkLicense}"
VALUES="${APK_SMOKE_VALUES:-1234 1111}"
EXPECTED_LOG="${APK_SMOKE_EXPECTED_LOG:-check(1234)=29711 check(1111)=19398}"
NATIVE_PKG_PATH="${NATIVE_PKG//.//}"
PKG_PATH="${PKG//.//}"
SDK_ROOT="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
BUILD_TOOLS="${ANDROID_BUILD_TOOLS:-$SDK_ROOT/build-tools/35.0.0}"
ANDROID_JAR="${ANDROID_JAR:-$SDK_ROOT/platforms/android-35/android.jar}"
AAPT2="$BUILD_TOOLS/aapt2"
D8="$BUILD_TOOLS/d8"
ZIPALIGN="$BUILD_TOOLS/zipalign"
APKSIGNER="$BUILD_TOOLS/apksigner"
KEYSTORE="${ANDROID_DEBUG_KEYSTORE:-$HOME/.android/debug.keystore}"

for tool in "$AAPT2" "$D8" "$ZIPALIGN" "$APKSIGNER"; do
  [[ -x "$tool" ]] || { echo "[!] missing Android build tool: $tool" >&2; exit 1; }
done
[[ -f "$ANDROID_JAR" ]] || { echo "[!] missing android.jar: $ANDROID_JAR" >&2; exit 1; }
[[ -f "$LIB_SO" ]] || { echo "[!] missing library: $LIB_SO" >&2; exit 1; }

read -r VALUE_A VALUE_B <<< "$VALUES"
[[ -n "${VALUE_A:-}" && -n "${VALUE_B:-}" ]] || { echo "[!] APK_SMOKE_VALUES must contain two integers" >&2; exit 1; }

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR/src/$PKG_PATH" "$OUT_DIR/src/$NATIVE_PKG_PATH" "$OUT_DIR"/res/values "$OUT_DIR"/lib/arm64-v8a "$OUT_DIR"/classes "$OUT_DIR"/dex "$HOME/.android"
cp "$LIB_SO" "$OUT_DIR/lib/arm64-v8a/lib${LIBRARY_NAME}.so"

cat > "$OUT_DIR/AndroidManifest.xml" <<EOF_MANIFEST
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="$PKG">
    <uses-sdk android:minSdkVersion="23" android:targetSdkVersion="35" />
    <application android:label="VMPackerSmoke" android:extractNativeLibs="true" android:theme="@style/AppTheme">
        <activity android:name=".$ACTIVITY_CLASS" android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
EOF_MANIFEST

cat > "$OUT_DIR/res/values/styles.xml" <<'EOF_STYLES'
<resources>
    <style name="AppTheme" parent="android:style/Theme.Material.Light.NoActionBar" />
</resources>
EOF_STYLES

cat > "$OUT_DIR/src/$NATIVE_PKG_PATH/$NATIVE_CLASS.java" <<EOF_NATIVE
package $NATIVE_PKG;

public final class $NATIVE_CLASS {
    static { System.loadLibrary("$LIBRARY_NAME"); }
    private $NATIVE_CLASS() {}
    public static native int $JNI_METHOD(int value);
}
EOF_NATIVE

cat > "$OUT_DIR/src/$PKG_PATH/$ACTIVITY_CLASS.java" <<EOF_ACTIVITY
package $PKG;

import android.app.Activity;
import android.os.Bundle;
import android.util.Log;
import android.widget.TextView;
import $NATIVE_PKG.$NATIVE_CLASS;

public class $ACTIVITY_CLASS extends Activity {
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        int a = $NATIVE_CLASS.$JNI_METHOD($VALUE_A);
        int b = $NATIVE_CLASS.$JNI_METHOD($VALUE_B);
        String msg = "check($VALUE_A)=" + a + " check($VALUE_B)=" + b;
        Log.i("VMPackerSmoke", msg);
        TextView tv = new TextView(this);
        tv.setText(msg);
        setContentView(tv);
    }
}
EOF_ACTIVITY

"$AAPT2" compile --dir "$OUT_DIR/res" -o "$OUT_DIR/res.zip"
"$AAPT2" link -o "$OUT_DIR/base.apk" -I "$ANDROID_JAR" --manifest "$OUT_DIR/AndroidManifest.xml" --java "$OUT_DIR/gen" "$OUT_DIR/res.zip"
javac -encoding UTF-8 -source 8 -target 8 -classpath "$ANDROID_JAR" -d "$OUT_DIR/classes" $(find "$OUT_DIR/gen" "$OUT_DIR/src" -name '*.java' | sort)
"$D8" --classpath "$ANDROID_JAR" --min-api 23 --output "$OUT_DIR/dex" $(find "$OUT_DIR/classes" -name '*.class' | sort)
cp "$OUT_DIR/base.apk" "$OUT_DIR/unsigned.apk"
(cd "$OUT_DIR/dex" && zip -q -u "$OUT_DIR/unsigned.apk" classes.dex)
(cd "$OUT_DIR" && zip -q -u unsigned.apk "lib/arm64-v8a/lib${LIBRARY_NAME}.so")
"$ZIPALIGN" -f -p 4 "$OUT_DIR/unsigned.apk" "$OUT_DIR/aligned.apk"

if [[ ! -f "$KEYSTORE" ]]; then
  keytool -genkeypair -keystore "$KEYSTORE" -storepass android -keypass android -alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=Android Debug,O=Android,C=US" >/dev/null
fi
"$APKSIGNER" sign --ks "$KEYSTORE" --ks-pass pass:android --key-pass pass:android --out "$OUT_DIR/smoke.apk" "$OUT_DIR/aligned.apk"
"$APKSIGNER" verify "$OUT_DIR/smoke.apk"

adb install -r "$OUT_DIR/smoke.apk" >/dev/null
adb logcat -c
adb shell am start -W -n "$PKG/$ACTIVITY" >/dev/null
sleep 1
adb logcat -d | grep 'VMPackerSmoke' | tail -5 | tee "$OUT_DIR/logcat.txt"
grep -q "$EXPECTED_LOG" "$OUT_DIR/logcat.txt"
echo "[+] APK smoke passed: $OUT_DIR/smoke.apk"
