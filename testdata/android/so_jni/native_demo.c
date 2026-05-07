typedef const struct JNINativeInterface_ *JNIEnv;
typedef void *jobject;

__attribute__((visibility("default")))
int Java_com_example_demo_NativeBridge_checkLicense(JNIEnv env, jobject thiz, int value) {
    (void)env;
    (void)thiz;
    int mixed = (value * 7) ^ 0x55AA;
    if ((mixed & 3) == 2) {
        mixed += 11;
    } else {
        mixed -= 5;
    }
    return mixed;
}
